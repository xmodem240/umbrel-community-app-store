"""Bitaxe Autotuner: finds safe overclock profiles for a Bitaxe (AxeOS), then keeps the miner
tuned live within the settings it proved stable.

Strategy
--------
Starting from the miner's current frequency/voltage, step frequency up. At each
point: apply settings, restart, warm up, then measure. A point is "stable" only if
hashrate tracks the expected value, hardware error rate and rejects stay low, and
no hard safety limit (temperature, VR temperature, power, input voltage) is hit.
When a voltage's ceiling is found, voltage is raised one step and the sweep
continues from the best stable frequency. The miner's original settings are
always restored when the run ends, is stopped, fails, or the container restarts.

Live guard (after the sweep)
----------------------------
The stable sweep points form a ladder of settings (one per frequency). The guard starts on the
"safe daily" profile and judges the miner in windows. It steps down one rung at once when errors
rise, temperatures get close to the limits, or the firmware throttles; it steps up one rung only
after several good windows in a row with temperature headroom, and never above the highest stable
point. A hard limit drops it straight to safe daily. Changes are applied live when the firmware
supports it (checked via actualFrequency) and by restarting the miner otherwise.
"""
import json
import sys
import os
import re
import statistics
import threading
import traceback
import time
from datetime import datetime, timezone

import requests
from flask import Flask, jsonify, request, send_from_directory
from werkzeug.exceptions import HTTPException

VERSION = "0.1.0"
DATA_DIR = os.environ.get("DATA_DIR", "/data")
# Factory defaults per board, generated from ESP-Miner's device_config.h (see tests/gen_boards.py).
# Used only when the miner's firmware is too old to report its own profile via /api/system/asic.
try:
    with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "boards.json")) as _f:
        BOARDS = json.load(_f)
except (OSError, ValueError):
    BOARDS = {"source": "", "boards": {}}
# NerdAxe-family boards (from the NerdQAxe firmware source) and spec-only models (e.g. Nexus S1).
try:
    with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "models_extra.json")) as _f:
        EXTRA = json.load(_f)
except (OSError, ValueError):
    EXTRA = {"nerd": {}, "spec": []}
STATE_FILE = os.path.join(DATA_DIR, "state.json")
HISTORY_FILE = os.path.join(DATA_DIR, "history.json")
MAX_SAMPLES = 6000
TEST_MODE = os.environ.get("TUNER_TEST_MODE") == "1"
SAMPLE_INTERVAL = float(os.environ.get("SAMPLE_INTERVAL", "5"))
RESTART_WAIT = float(os.environ.get("RESTART_WAIT", "8"))
HTTP_TIMEOUT = 8

# key, label, unit, default, min, max, help   (min/max are hard safety rails)
SCHEMA = [
    dict(key="freq_step", label="Frequency step", unit="MHz", default=10, min=5, max=50,
         help="Increase per test."),
    dict(key="volt_step", label="Voltage step", unit="mV", default=10, min=5, max=50,
         help="Increase when a voltage's frequency ceiling is found."),
    dict(key="start_freq", label="Start frequency", unit="MHz", default=0, min=0, max=1000,
         help="0 = the miner's current frequency."),
    dict(key="start_voltage", label="Start core voltage", unit="mV", default=0, min=0, max=1400,
         help="0 = the miner's current core voltage."),
    dict(key="max_freq", label="Max frequency", unit="MHz", default=650, min=100, max=1000,
         help="Never test above this."),
    dict(key="max_voltage", label="Max core voltage", unit="mV", default=1300, min=800, max=1400,
         help="Never test above this."),
    dict(key="max_temp", label="Max ASIC temp", unit="°C", default=66, min=40, max=72,
         help="Hard stop. Exceeding this ends the point as thermal-limited."),
    dict(key="max_vr_temp", label="Max VR temp", unit="°C", default=82, min=50, max=90,
         help="Hard stop for the voltage regulator."),
    dict(key="max_power", label="Max power", unit="W", default=30, min=5, max=400,
         help="Hard stop. Set to what your board can safely take, not just the PSU rating "
              "(5 V single-chip ~25-30 W; 12 V multi-chip boards like the Naja Duo ~100 W on air)."),
    dict(key="min_input_mv", label="Min input voltage", unit="mV", default=4500, min=3000, max=13000,
         help="Abort if the supply sags below this (brown-out risk)."),
    dict(key="max_input_mv", label="Max input voltage", unit="mV", default=14000, min=4000, max=16000,
         help="Abort above this."),
    dict(key="warmup_s", label="Warm-up per point", unit="s", default=120, min=30, max=900,
         help="Settle time before measuring."),
    dict(key="measure_s", label="Measure per point", unit="s", default=180, min=30, max=1200,
         help="Averaging window."),
    dict(key="min_hash_ratio", label="Min hashrate / expected", unit="", default=0.93, min=0.5, max=1.0,
         help="Below this the point is unstable."),
    dict(key="max_error_pct", label="Max ASIC error rate", unit="%", default=2.0, min=0.1, max=20,
         help="Needs firmware that reports errorPercentage."),
    dict(key="max_reject_pct", label="Max rejected shares", unit="%", default=5.0, min=0.5, max=50,
         help="Share reject ratio during the window."),
    dict(key="retries", label="Retests on instability", unit="", default=1, min=0, max=3,
         help="Re-run an unstable point to rule out flukes."),
    dict(key="margin_steps", label="Safety margin", unit="steps", default=1, min=0, max=5,
         help="'Safe daily' profile sits this many frequency steps below the ceiling."),
    dict(key="guard_window_s", label="Guard window", unit="s", default=900, min=120, max=3600,
         help="Live guard: how long each judgement window lasts."),
    dict(key="guard_up_windows", label="Good windows to step up", unit="", default=4, min=1, max=24,
         help="Live guard: consecutive good windows needed before trying one step higher."),
    dict(key="guard_temp_margin", label="Step-down temp margin", unit="°C", default=4, min=1, max=15,
         help="Live guard: step down at once when ASIC or VR temperature gets this close to its limit."),
    dict(key="guard_up_headroom", label="Step-up temp headroom", unit="°C", default=8, min=2, max=25,
         help="Live guard: only step up when the window stayed at least this far under the temp limits."),
]
SCHEMA_BY_KEY = {s["key"]: s for s in SCHEMA}
DEFAULTS = {s["key"]: s["default"] for s in SCHEMA}
DEFAULTS["host"] = ""
DEFAULTS["cooling"] = "air"

if TEST_MODE:  # allow short timings in the simulator
    SCHEMA_BY_KEY["warmup_s"]["min"] = 0
    SCHEMA_BY_KEY["measure_s"]["min"] = 1
    SCHEMA_BY_KEY["guard_window_s"]["min"] = 1

GUARD_SETTLE_LIVE = float(os.environ.get("GUARD_SETTLE_LIVE", "60"))   # s after a live change before judging
GUARD_CONFIRM_S = float(os.environ.get("GUARD_CONFIRM_S", "30"))       # s to wait for actualFrequency to match
GUARD_FAST_S = float(os.environ.get("GUARD_FAST_S", "120"))            # rolling error-rate window for fast step-down
GUARD_MAX_MISSES = 24          # consecutive failed samples before the guard gives up (~2 min at 5 s)
GUARD_MAX_EVENTS = 200
GUARD_MAX_FAILS = 3            # each failure at a rung doubles the windows needed to return (max 8x)

HOST_RE = re.compile(r"^[A-Za-z0-9._\-]+(:\d{1,5})?$")


def now():
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def clean_host(h):
    h = (h or "").strip()
    h = re.sub(r"^https?://", "", h).rstrip("/")
    if h and not HOST_RE.match(h):
        raise ValueError("Host must be an IP or hostname, e.g. 192.168.1.50")
    return h


def validate_config(raw):
    cfg = dict(DEFAULTS)
    for k, v in raw.items():
        if k == "host":
            cfg["host"] = clean_host(v)
            continue
        if k == "cooling":
            if v not in ("air", "water"):
                raise ValueError("Cooling must be air or water")
            cfg["cooling"] = v
            continue
        s = SCHEMA_BY_KEY.get(k)
        if not s:
            continue
        cfg[k] = coerce(s, v)
    check_cross(cfg)
    return cfg


def coerce(s, v):
    """Validate one numeric setting. Integer settings have int defaults; anything with a float
    default (e.g. 2.0 %) keeps its decimals, so 1.5 % stays 1.5 % rather than becoming 1 %."""
    try:
        n = float(v)
    except (TypeError, ValueError):
        raise ValueError(f"{s['label']} must be a number")
    if n != n or not (s["min"] <= n <= s["max"]):
        raise ValueError(f"{s['label']} must be between {s['min']} and {s['max']}")
    return int(round(n)) if isinstance(s["default"], int) else n


def check_cross(cfg):
    if cfg["min_input_mv"] >= cfg["max_input_mv"]:
        raise ValueError("Min input voltage must be below max input voltage")


# Settings that can be changed while a run is in progress. Hard limits are checked on every
# sample, pass/fail criteria when the current point is judged, timings/steps from the next point.
LIVE_KEYS = ["max_temp", "max_vr_temp", "max_power", "min_input_mv", "max_input_mv",
             "min_hash_ratio", "max_error_pct", "max_reject_pct", "retries",
             "warmup_s", "measure_s", "freq_step", "volt_step", "max_freq", "max_voltage",
             "margin_steps", "guard_window_s", "guard_up_windows", "guard_temp_margin",
             "guard_up_headroom"]


class Bitaxe:
    def __init__(self, host):
        self.base = f"http://{host}"

    def info(self):
        r = requests.get(self.base + "/api/system/info", timeout=HTTP_TIMEOUT)
        r.raise_for_status()
        return r.json()

    def set(self, freq, volt):
        r = requests.patch(self.base + "/api/system",
                           json={"frequency": int(freq), "coreVoltage": int(volt)},
                           timeout=HTTP_TIMEOUT)
        r.raise_for_status()

    def asic(self):
        """Factory profile reported by the miner itself (ESP-Miner 2.x+). None on older firmware."""
        try:
            r = requests.get(self.base + "/api/system/asic", timeout=HTTP_TIMEOUT)
            if r.status_code != 200:
                return None
            d = r.json()
            return d if isinstance(d, dict) and d.get("defaultFrequency") else None
        except (requests.RequestException, ValueError):
            return None

    def restart(self):
        try:
            requests.post(self.base + "/api/system/restart", timeout=HTTP_TIMEOUT)
        except requests.RequestException:
            pass  # device may drop the connection while rebooting


class Aborted(Exception):
    pass


class Tuner:
    def __init__(self):
        self.lock = threading.RLock()
        self.stop_ev = threading.Event()
        self.thread = None
        self.dev = None
        self.k = None  # fallback GH/s per MHz
        self.state = {
            "config": dict(DEFAULTS),
            "run": {"status": "idle", "message": "", "phase": "", "current": None,
                    "started": None, "finished": None},
            "device": None,
            "profile": None,
            "original": None,
            "results": [],
            "telemetry": None,
            "log": [],
            "guard": None,
        }
        self.history = []
        self.markers = []
        self.epoch = int(time.time() * 1000)
        self._hist_saved = 0.0
        self.restore_lock = threading.Lock()
        self._dirty = False          # True while the miner may be on a test setting
        self.emergency_flag = False
        self.storage_error = None
        try:
            os.makedirs(DATA_DIR, exist_ok=True)
        except OSError:
            pass
        self._load()
        self._load_history()
        self._recover()

    # ---------- persistence / logging ----------
    def _load(self):
        try:
            with open(STATE_FILE) as f:
                saved = json.load(f)
            self.state.update(saved)
            self.state["config"] = {**DEFAULTS, **self.state.get("config", {})}
        except (OSError, ValueError):
            pass

    def _save(self):
        """Persist state. A storage failure must never break a run (or the safety restore),
        so it is recorded and shown in the UI instead of raised."""
        with self.lock:
            try:
                tmp = STATE_FILE + ".tmp"
                with open(tmp, "w") as f:
                    json.dump(self.state, f)
                os.replace(tmp, STATE_FILE)
                self.storage_error = None
            except OSError as e:
                if not self.storage_error:
                    print(f"WARNING: cannot write {STATE_FILE}: {e}", file=sys.stderr, flush=True)
                self.storage_error = (f"Can't save to the app's data folder ({e.strerror}). Tuning still works and "
                                      "the miner is still restored when a run ends, but if the app restarts "
                                      "mid-run it can't recover. Reinstalling the app usually fixes this.")

    def log(self, msg):
        with self.lock:
            self.state["log"].append(f"{datetime.now().strftime('%H:%M:%S')}  {msg}")
            self.state["log"] = self.state["log"][-400:]
        self._save()

    def _set_run(self, **kw):
        with self.lock:
            self.state["run"].update(kw)
        self._save()

    def _recover(self):
        """If we died mid-run, the miner may be left on a test setting: restore it.
        If we died while the live guard was running (not while stopping), resume the guard
        from the safe daily profile instead, so the miner stays guarded after a reboot."""
        run = self.state["run"]
        g = self.state.get("guard")
        if run.get("status") == "running" and g and g.get("active") and g.get("ladder") \
                and self.state.get("original") and self.state["config"].get("host"):
            self.dev = Bitaxe(self.state["config"]["host"])
            self._dirty = True            # the miner may be on any rung: restore on stop
            self.stop_ev.clear()
            self.log("App restarted while the live guard was running: resuming from the safe daily profile.")
            self.thread = threading.Thread(target=self._resume_guard, daemon=True)
            self.thread.start()
            return
        if run.get("status") in ("running", "stopping"):
            orig, host = self.state.get("original"), self.state["config"].get("host")
            msg = "Interrupted by a restart."
            if orig and host:
                try:
                    d = Bitaxe(host)
                    d.set(orig["freq"], orig["volt"])
                    d.restart()
                    msg += f" Original settings ({orig['freq']} MHz / {orig['volt']} mV) restored."
                except Exception as e:  # noqa: BLE001
                    msg += (f" COULD NOT restore original settings ({orig['freq']} MHz / "
                            f"{orig['volt']} mV): {e}. Set them manually in AxeOS.")
            self.state["run"].update(status="interrupted", message=msg, phase="", current=None,
                                     finished=now())
            self.log(msg)

    # ---------- history (for charts) ----------
    def _load_history(self):
        try:
            with open(HISTORY_FILE) as f:
                h = json.load(f)
            self.history = h.get("samples", [])[-MAX_SAMPLES:]
            self.markers = h.get("markers", [])
            self.epoch = h.get("epoch", self.epoch)
        except (OSError, ValueError):
            pass

    def _save_history(self, force=False):
        if not force and time.time() - self._hist_saved < 20:
            return
        with self.lock:
            blob = json.dumps({"epoch": self.epoch, "samples": self.history, "markers": self.markers},
                              separators=(",", ":"))
        try:
            tmp = HISTORY_FILE + ".tmp"
            with open(tmp, "w") as f:
                f.write(blob)
            os.replace(tmp, HISTORY_FILE)
            self._hist_saved = time.time()
        except OSError:
            pass

    def _record(self, s, freq, volt):
        row = {"t": round(s["t"], 1), "hash": round(s["hash"], 1), "temp": s["temp"], "vr": s["vr"],
               "power": s["power"], "freq": freq, "volt": volt, "exp": s["expected"] or None,
               "err": s["err"]}
        with self.lock:
            self.history.append(row)
            if len(self.history) > MAX_SAMPLES:
                del self.history[:len(self.history) - MAX_SAMPLES]
        self._save_history()

    def _mark(self, freq, volt):
        with self.lock:
            self.markers.append({"t": round(time.time(), 1), "freq": freq, "volt": volt,
                                 "n": len(self.state["results"]) + 1})

    def clear_history(self):
        with self.lock:
            self.history, self.markers = [], []
            self.epoch = int(time.time() * 1000)
        self._save_history(force=True)

    # ---------- public API ----------
    @property
    def active(self):
        return self.thread is not None and self.thread.is_alive()

    def start(self, cfg):
        with self.lock:
            if self.active:
                raise RuntimeError("A run is already in progress")
            if not cfg["host"]:
                raise ValueError("Enter your Bitaxe's IP address first")
            self.state["config"] = cfg
            self.state["results"] = []
            self.state["log"] = []
            self.state["telemetry"] = None
            self.state["guard"] = None
            self.stop_ev.clear()
            self.k = None
            self.state["original"] = None   # never restore a stale value from an earlier run
            self.emergency_flag = False
            self._dirty = False
            self.dev = Bitaxe(cfg["host"])
        self.clear_history()
        self._set_run(status="running", message="Starting…", phase="init", current=None, point=0,
                      started=now(), finished=None)
        self.thread = threading.Thread(target=self._run, daemon=True)
        self.thread.start()

    def stop(self):
        if self.active:
            self.stop_ev.set()
            self._set_run(status="stopping", message="Stopping and restoring original settings…")

    def emergency(self):
        """Immediate stop: don't wait for the run loop, put the original settings back right now."""
        if not self.active:
            raise RuntimeError("No run in progress")
        self.emergency_flag = True
        self.stop_ev.set()
        self._set_run(status="stopping", message="EMERGENCY STOP: restoring original settings…")
        self.log("EMERGENCY STOP pressed")
        threading.Thread(target=self._restore, daemon=True).start()

    def restore_original(self):
        if self.active:
            raise RuntimeError("A run is in progress. Use Emergency stop.")
        orig = self.state.get("original")
        host = self.state["config"].get("host")
        if not orig or not host:
            raise ValueError("No saved original settings to restore")
        d = Bitaxe(host)
        d.set(orig["freq"], orig["volt"])
        d.restart()
        self.log(f"Restored original settings ({orig['freq']} MHz / {orig['volt']} mV) on request")
        return orig

    def update_live(self, raw):
        """Change limits during a run. The config dict is updated in place, so the run loop,
        the per-sample safety checks and the per-point verdict all see the new values."""
        if not isinstance(raw, dict) or not raw:
            raise ValueError("Nothing to change")
        locked = [k for k in raw if k not in LIVE_KEYS]
        if locked:
            other = {"host": "Miner address", "cooling": "Cooling"}
            names = ", ".join(SCHEMA_BY_KEY[k]["label"] if k in SCHEMA_BY_KEY else other.get(k, k) for k in locked)
            raise ValueError(f"{names} can't be changed during a run. Stop and start a new run instead.")
        new = {k: coerce(SCHEMA_BY_KEY[k], v) for k, v in raw.items()}
        with self.lock:
            cfg = self.state["config"]
            check_cross({**cfg, **new})
            diffs = [(k, cfg.get(k), v) for k, v in new.items() if cfg.get(k) != v]
            cfg.update(new)
        if diffs:
            self.log("Settings changed during run: " + "; ".join(
                f"{SCHEMA_BY_KEY[k]['label']} {old} → {v}{(' ' + SCHEMA_BY_KEY[k]['unit']) if SCHEMA_BY_KEY[k]['unit'] else ''}"
                for k, old, v in diffs))
        self._save()
        return [{"key": k, "old": old, "new": v} for k, old, v in diffs]

    def apply(self, host, freq, volt):
        if self.active:
            raise RuntimeError("Can't apply while a run is in progress")
        host = clean_host(host)
        cfg = self.state["config"]
        if not (100 <= freq <= cfg["max_freq"] and 800 <= volt <= cfg["max_voltage"]):
            raise ValueError("Values outside the configured limits")
        d = Bitaxe(host)
        d.set(freq, volt)
        d.restart()
        self.log(f"Applied {freq} MHz / {volt} mV to {host}")

    # ---------- engine ----------
    def _sleep(self, secs):
        if self.stop_ev.wait(secs):
            raise Aborted()

    def _sample(self):
        i = self.dev.info()
        return {
            "t": time.time(), "hash": float(i.get("hashRate") or 0),
            "temp": hottest_chip(i), "temp2": i.get("temp2"), "vr": i.get("vrTemp"), "power": i.get("power"),
            "overheat": i.get("overheat_mode"),
            "vin": i.get("voltage"), "err": i.get("errorPercentage"),
            "acc": i.get("sharesAccepted") or 0, "rej": i.get("sharesRejected") or 0,
            "fan": i.get("fanspeed") if i.get("fanspeed") is not None else i.get("fanrpm"),
            "freq": i.get("frequency"), "volt": i.get("coreVoltage"),
            "afreq": i.get("actualFrequency"), "avolt": i.get("coreVoltageActual"),
            "expected": i.get("expectedHashrate"),
            "cores": (i.get("smallCoreCount") or 0) * (i.get("asicCount") or 0),
        }

    def _hard_limit(self, s):
        c = self.state["config"]
        if s.get("overheat") in (1, True):
            return "thermal", "the miner's firmware entered overheat mode"
        if s["temp"] is not None and s["temp"] >= c["max_temp"]:
            return "thermal", f"ASIC {s['temp']:.1f}°C ≥ {c['max_temp']}°C"
        if s["vr"] is not None and s["vr"] >= c["max_vr_temp"]:
            return "thermal", f"VR {s['vr']:.1f}°C ≥ {c['max_vr_temp']}°C"
        if s["power"] is not None and s["power"] >= c["max_power"]:
            return "power", f"{s['power']:.1f} W ≥ {c['max_power']} W"
        if s["vin"]:
            if s["vin"] < c["min_input_mv"]:
                return "input", f"input {s['vin']} mV below {c['min_input_mv']} mV"
            if s["vin"] > c["max_input_mv"]:
                return "input", f"input {s['vin']} mV above {c['max_input_mv']} mV"
        return None

    def _apply_and_wait(self, freq, volt):
        self._set_run(phase="applying", message=f"Testing {freq} MHz / {volt} mV",
                      current={"freq": freq, "volt": volt, "elapsed": 0, "total": 0})
        if self.stop_ev.is_set():
            raise Aborted()
        self._dirty = True
        try:
            self.dev.set(freq, volt)
        finally:
            self._dirty = True   # set again after the call: a late PATCH must never go unrestored
        if self.stop_ev.is_set():
            raise Aborted()
        self.dev.restart()
        self._sleep(RESTART_WAIT)
        deadline = time.time() + 150
        while True:
            try:
                s = self._sample()
                self._record(s, freq, volt)
                break
            except requests.RequestException:
                if time.time() > deadline:
                    raise RuntimeError("Miner did not come back after restart")
                self._sleep(3)
        if s["freq"] is not None and s["volt"] is not None and \
                (abs(s["freq"] - freq) > 1 or abs(s["volt"] - volt) > 1):
            raise RuntimeError(
                f"Miner reports {s['freq']} MHz / {s['volt']} mV, expected {freq} / {volt}. "
                "It rejected or clamped the setting; this is usually the firmware's own maximum. "
                "Lower Max frequency / Max core voltage.")

    def _measure_point(self, freq, volt):
        c = self.state["config"]
        self._mark(freq, volt)
        self._apply_and_wait(freq, volt)
        total = c["warmup_s"] + c["measure_s"]
        t0 = time.time()
        samples, fails = [], 0
        hard = None
        while True:
            elapsed = time.time() - t0
            if elapsed >= total:
                break
            phase = "warmup" if elapsed < c["warmup_s"] else "measuring"
            self._set_run(phase=phase, current={"freq": freq, "volt": volt,
                                                "elapsed": int(elapsed), "total": int(total)})
            try:
                s = self._sample()
                fails = 0
            except requests.RequestException:
                fails += 1
                if fails >= 4:
                    raise RuntimeError("Lost contact with the miner")
                self._sleep(SAMPLE_INTERVAL)
                continue
            with self.lock:
                self.state["telemetry"] = s
            self._record(s, freq, volt)
            hard = self._hard_limit(s)
            if hard:
                break
            if elapsed >= c["warmup_s"]:
                samples.append(s)
            self._sleep(SAMPLE_INTERVAL)

        res = {"freq": freq, "volt": volt, "time": now()}
        if hard:
            res.update(status=hard[0], reason=hard[1])
            if samples:
                res.update(self._summarize(samples, freq))
            else:
                res.update(hash=None, temp=s["temp"], vr=s["vr"], power=s["power"], jth=None)
            return res
        if not samples:
            res.update(status="unstable", reason="no samples collected", hash=None)
            return res
        res.update(self._summarize(samples, freq))
        if res["hash"] < 1:
            res.update(status="unstable", reason="no hashrate")
        elif res["ratio"] is not None and res["ratio"] < c["min_hash_ratio"]:
            res.update(status="unstable",
                       reason=f"hashrate {res['ratio']*100:.1f}% of expected (min {c['min_hash_ratio']*100:.0f}%)")
        elif res["err"] is not None and res["err"] > c["max_error_pct"]:
            res.update(status="unstable", reason=f"ASIC errors {res['err']:.2f}% > {c['max_error_pct']}%")
        elif res["reject_pct"] > c["max_reject_pct"]:
            res.update(status="unstable", reason=f"rejects {res['reject_pct']:.1f}% > {c['max_reject_pct']}%")
        else:
            res.update(status="stable", reason="")
        return res

    def _summarize(self, samples, freq):
        def avg(key):
            v = [s[key] for s in samples if s[key] is not None]
            return statistics.fmean(v) if v else None

        def mx(key):
            v = [s[key] for s in samples if s[key] is not None]
            return max(v) if v else None

        hashrate = avg("hash") or 0
        power = avg("power")
        exp = next((s["expected"] for s in samples if s["expected"]), None)
        if not exp:
            cores = samples[-1]["cores"]
            exp = freq * cores / 1000 if cores else None
        if not exp:  # learn GH/s per MHz from the first measurement
            if self.k is None and hashrate > 0:
                self.k = hashrate / freq
            exp = self.k * freq if self.k else None
        d_acc = samples[-1]["acc"] - samples[0]["acc"]
        d_rej = samples[-1]["rej"] - samples[0]["rej"]
        tot = d_acc + d_rej
        return {
            "hash": round(hashrate, 1), "expected": round(exp, 1) if exp else None,
            "ratio": round(hashrate / exp, 4) if exp else None,
            "temp": round(avg("temp"), 1) if avg("temp") is not None else None,
            "temp_max": mx("temp"), "vr": round(avg("vr"), 1) if avg("vr") is not None else None,
            "power": round(power, 2) if power is not None else None,
            "err": round(avg("err"), 3) if avg("err") is not None else None,
            "reject_pct": round(100 * d_rej / tot, 2) if tot >= 5 else 0.0,
            "jth": round(power / (hashrate / 1000), 2) if power and hashrate > 0 else None,
        }

    def _test_point(self, freq, volt):
        self._set_run(point=len(self.state["results"]) + 1)
        retries = self.state["config"]["retries"]
        for attempt in range(retries + 1):
            self.log(f"Testing {freq} MHz / {volt} mV" + (f" (retest {attempt})" if attempt else ""))
            res = self._measure_point(freq, volt)
            if res["status"] != "unstable":
                break
            self.log(f"  unstable: {res['reason']}")
        with self.lock:
            self.state["results"].append(res)
        self.log(f"  → {res['status'].upper()}"
                 + (f" {res['hash']} GH/s, {res['temp']}°C, {res['power']} W, {res['jth']} J/TH"
                    if res.get("hash") else f": {res.get('reason', '')}"))
        self._save()
        self._save_history(force=True)
        return res

    def _run(self):
        c = self.state["config"]
        status, message = "done", "Finished."
        self.dev = Bitaxe(c["host"])
        try:
            info = self.dev.info()
            orig = {"freq": int(info["frequency"]), "volt": int(info["coreVoltage"])}
            with self.lock:
                self.state["original"] = orig
                self.state["device"] = self._device_summary(info)
            self._save()
            self.log(f"Original settings: {orig['freq']} MHz / {orig['volt']} mV (will be restored)")

            volt = c["start_voltage"] or orig["volt"]
            freq = c["start_freq"] or orig["freq"]
            best_prev = None
            first = True
            while volt <= c["max_voltage"]:
                best_here, hit_hard, hit_max = None, None, False
                f = freq
                while True:
                    if f > c["max_freq"]:
                        hit_max = True
                        break
                    res = self._test_point(f, volt)
                    if res["status"] == "stable":
                        best_here = f
                        f += c["freq_step"]
                    elif res["status"] == "unstable":
                        break
                    else:
                        hit_hard = res
                        break
                if hit_hard:   # a safety stop always explains itself, even on the first point
                    message = f"Stopped at a safety limit ({hit_hard['reason']})."
                    break
                if best_here is None:
                    message = ("The starting point was unstable; lower the start frequency/voltage."
                               if first else "No stable gain at the higher voltage; sweep complete.")
                    break
                first = False
                if hit_max:
                    message = "Reached the configured max frequency without instability."
                    break
                if best_prev is not None and best_here <= best_prev:
                    message = "More voltage no longer raises the stable frequency; sweep complete."
                    break
                best_prev = best_here
                freq = best_here + c["freq_step"]
                volt += c["volt_step"]
            else:
                message = "Reached the configured max voltage."
            self.log("Sweep: " + message)
            message = self._start_guard(message)
        except Aborted:
            status, message = "stopped", "Stopped by you."
        except Exception as e:  # noqa: BLE001
            if self.stop_ev.is_set():      # errors caused by our own emergency restore are not errors
                status, message = "stopped", "Stopped by you."
            else:
                status, message = "error", str(e)
        finally:
            self._finish(status, message)

    def _resume_guard(self):
        status, message = "done", "Finished."
        try:
            self._guard(resume=True)
        except Aborted:
            status, message = "stopped", "Stopped by you."
        except Exception as e:  # noqa: BLE001
            if self.stop_ev.is_set():
                status, message = "stopped", "Stopped by you."
            else:
                status, message = "error", str(e)
        finally:
            self._finish(status, message)

    def _finish(self, status, message):
        """End of a run (sweep or guard): always put the original settings back."""
        with self.lock:
            if self.state.get("guard"):
                self.state["guard"]["active"] = False
        restored = self._restore()
        if not restored:
            status = "error"
            message += " WARNING: could not restore the original settings. Check the miner in AxeOS!"
        elif self.emergency_flag and status == "stopped":
            message = "Emergency stop: original settings restored."
        self._save_history(force=True)
        self.log(message)
        self._set_run(status=status, message=message, phase="", current=None, finished=now())

    # ---------- live guard ----------
    def _start_guard(self, sweep_message):
        """Called when the sweep ends normally. Starts the guard if the sweep produced a safe daily
        profile; otherwise the run ends (and the original settings are restored) as before."""
        with self.lock:
            c = self.state["config"]
            recs = recommend(self.state["results"], c)
            safe = recs.get("safe_daily")
            if not safe:
                return sweep_message + " No safe daily profile was found, so the live guard did not start."
            ladder, safe_i = build_ladder(self.state["results"], safe)
            self.state["guard"] = {"active": True, "mode": None, "ladder": ladder, "safe": safe_i,
                                   "top": len(ladder) - 1, "index": safe_i, "good": 0, "fails": {},
                                   "events": [], "time_at": {}, "since_change": None, "settle": 0,
                                   "started": now(), "window": None}
        self.log(f"Live guard starting on safe daily {safe['freq']} MHz / {safe['volt']} mV; "
                 f"{len(ladder)} tested-stable settings from {ladder[0]['freq']} to {ladder[-1]['freq']} MHz.")
        self._guard()
        return sweep_message   # not reached: the guard only ends by stop or error

    def _guard_event(self, g, kind, frm, to, reason):
        ev = {"t": round(time.time(), 1), "kind": kind, "reason": reason,
              "from": None if frm is None else {"freq": g["ladder"][frm]["freq"], "volt": g["ladder"][frm]["volt"]},
              "to": None if to is None else {"freq": g["ladder"][to]["freq"], "volt": g["ladder"][to]["volt"]}}
        with self.lock:
            g["events"].append(ev)
            del g["events"][:-GUARD_MAX_EVENTS]

    def _guard_need(self, g, j):
        """Good windows needed before stepping up to rung j."""
        c = self.state["config"]
        n = c["guard_up_windows"] * 2 ** min(g["fails"].get(str(j), 0), GUARD_MAX_FAILS)
        return n * (2 if g["mode"] == "restart" else 1)

    def _guard_allowed(self, g, j):
        c = self.state["config"]
        r = g["ladder"][j]
        return r["freq"] <= c["max_freq"] and r["volt"] <= c["max_voltage"]

    def _guard_apply(self, g, i, kind, reason):
        """Move the miner to rung i: live when the firmware applies it, by restart otherwise."""
        c = self.state["config"]
        frm = g["index"] if kind != "start" else None
        r = g["ladder"][i]
        f, v = r["freq"], r["volt"]
        self._set_run(phase="applying", message=f"Live guard: {kind} to {f} MHz / {v} mV")
        if self.stop_ev.is_set():
            raise Aborted()
        live_ok = False
        if g["mode"] != "restart":
            self._dirty = True
            try:
                self.dev.set(f, v)
            finally:
                self._dirty = True
            deadline = time.time() + GUARD_CONFIRM_S
            no_field = False
            while True:
                try:
                    smp = self._sample()
                    if smp["afreq"] is None:
                        no_field = True
                        break
                    if abs(float(smp["afreq"]) - f) <= 2 and (smp["volt"] is None or abs(smp["volt"] - v) <= 1):
                        live_ok = True
                        break
                except requests.RequestException:
                    pass
                if time.time() > deadline:
                    break
                self._sleep(min(3.0, max(0.2, SAMPLE_INTERVAL)))
            if not live_ok:
                why = ("the firmware doesn't report its live frequency" if no_field
                       else f"the live frequency didn't reach {f} MHz within {GUARD_CONFIRM_S:.0f} s")
                self.log(f"Live guard: changes can't be applied live here ({why}); switching to restart "
                         "mode, which restarts the miner for each change and steps up half as often.")
                g["mode"] = "restart"
        if live_ok:
            g["mode"] = "live"
            g["settle"] = GUARD_SETTLE_LIVE
        else:
            self._apply_and_wait(f, v)       # restart, wait, verify the miner kept the values
            g["settle"] = c["warmup_s"]
        with self.lock:
            g["index"] = i
            g["good"] = 0
            g["since_change"] = time.time()
            self.markers.append({"t": round(time.time(), 1), "freq": f, "volt": v, "n": 0, "guard": kind})
            del self.markers[:-500]
        self._guard_event(g, kind, frm, i, reason)
        self.log(f"Live guard {kind.upper()}: {f} MHz / {v} mV ({g['mode']}). {reason}")
        self._save()

    def _guard_down(self, g, reason, failure):
        """Step down one rung. Returns False when already on the lowest tested setting."""
        i = g["index"]
        if failure:
            g["fails"][str(i)] = g["fails"].get(str(i), 0) + 1
        if i == 0:
            g["good"] = 0
            last = g.get("bottom_warned") or 0
            if time.time() - last >= self.state["config"]["guard_window_s"]:
                g["bottom_warned"] = time.time()
                self._guard_event(g, "hold", i, i, "At the lowest tested setting, can't step down: " + reason)
                self.log("Live guard: at the lowest tested setting, can't step down further (" + reason + ")")
            return False
        self._guard_apply(g, i - 1, "down", reason)
        return True

    def _guard_hard(self, g, reason):
        """A hard limit: go straight to safe daily (or one rung down when already at or below it)."""
        i = g["index"]
        g["fails"][str(i)] = g["fails"].get(str(i), 0) + 1
        if i > g["safe"]:
            self._guard_apply(g, g["safe"], "down", f"Hard limit: {reason}. Back to safe daily.")
        elif i > 0:
            self._guard_apply(g, i - 1, "down", f"Hard limit: {reason}.")
        else:
            raise RuntimeError(f"Hard limit at the lowest tested setting ({reason}). "
                               "The live guard stopped and the original settings were restored.")

    def _guard(self, resume=False):
        g = self.state["guard"]
        c = self.state["config"]
        with self.lock:
            g["active"] = True
        if resume:
            g["mode"] = g.get("mode") if g.get("mode") == "restart" else None
            self._guard_apply(g, g["safe"], "start", "Resumed after an app restart.")
        else:
            self._guard_apply(g, g["index"], "start", "Sweep finished; guarding from safe daily.")
        while True:
            wlen = c["guard_window_s"]
            wstart = time.time()
            with self.lock:
                g["window"] = {"started": round(wstart, 1), "length": wlen}
            samples, misses, hot, thr = [], 0, 0, 0
            last_t = time.time()
            changed = False
            while time.time() - wstart < wlen:
                if self.stop_ev.is_set():
                    raise Aborted()
                rung = g["ladder"][g["index"]]
                elapsed = time.time() - wstart
                self._set_run(phase="guard", message=f"Live guard on {rung['freq']} MHz / {rung['volt']} mV",
                              current={"freq": rung["freq"], "volt": rung["volt"],
                                       "elapsed": int(elapsed), "total": int(wlen)})
                try:
                    smp = self._sample()
                    misses = 0
                except requests.RequestException:
                    misses += 1
                    if misses >= GUARD_MAX_MISSES:
                        raise RuntimeError("Lost contact with the miner for about two minutes")
                    self._sleep(SAMPLE_INTERVAL)
                    continue
                t = time.time()
                with self.lock:
                    self.state["telemetry"] = smp
                    k = str(g["index"])
                    g["time_at"][k] = round(g["time_at"].get(k, 0) + min(t - last_t, SAMPLE_INTERVAL * 3), 1)
                last_t = t
                self._record(smp, rung["freq"], rung["volt"])

                if smp.get("overheat") in (1, True):
                    raise RuntimeError("The miner's firmware entered overheat mode. The live guard stopped "
                                       "and the original settings were restored.")
                hard = self._hard_limit(smp)
                if hard:
                    self._guard_hard(g, hard[1])
                    changed = True
                    break
                # The firmware lowers its saved frequency itself when it throttles for heat.
                thr = thr + 1 if smp["freq"] is not None and abs(smp["freq"] - rung["freq"]) > 1 else 0
                if thr >= 2:
                    fw = smp["freq"]
                    below = [j for j, r in enumerate(g["ladder"]) if r["freq"] <= fw]
                    if not below:
                        raise RuntimeError(f"The firmware throttled to {fw:g} MHz, below the lowest tested setting. "
                                           "The live guard stopped and the original settings were restored.")
                    g["fails"][str(g["index"])] = g["fails"].get(str(g["index"]), 0) + 1
                    self._guard_apply(g, min(max(below), g["safe"]), "down",
                                      f"Firmware throttled to {fw:g} MHz.")
                    changed = True
                    break
                if not self._guard_allowed(g, g["index"]):
                    ok = [j for j in range(g["index"]) if self._guard_allowed(g, j)]
                    if not ok:
                        raise RuntimeError("No tested setting fits under the current Max frequency / Max core "
                                           "voltage. The live guard stopped and the original settings were restored.")
                    self._guard_apply(g, max(ok), "down", "Max frequency / Max core voltage was lowered.")
                    changed = True
                    break
                if g["since_change"] and t - g["since_change"] < g["settle"]:
                    self._sleep(SAMPLE_INTERVAL)
                    continue
                samples.append(smp)
                m = c["guard_temp_margin"]
                warm = ((smp["temp"] is not None and smp["temp"] >= c["max_temp"] - m)
                        or (smp["vr"] is not None and smp["vr"] >= c["max_vr_temp"] - m))
                hot = hot + 1 if warm else 0
                if hot >= 2:
                    which = (f"ASIC {smp['temp']:.1f}°C" if smp["temp"] is not None and smp["temp"] >= c["max_temp"] - m
                             else f"VR {smp['vr']:.1f}°C")
                    if self._guard_down(g, f"{which} within {m}°C of its limit.", failure=False):
                        changed = True
                        break
                    hot = 0
                recent = [x for x in samples if x["t"] >= t - GUARD_FAST_S and x["err"] is not None]
                if recent and t - recent[0]["t"] >= GUARD_FAST_S * 0.8:
                    e = statistics.fmean(x["err"] for x in recent)
                    if e > c["max_error_pct"]:
                        if self._guard_down(g, f"ASIC errors {e:.2f}% over the last {GUARD_FAST_S:.0f} s "
                                               f"(limit {c['max_error_pct']}%).", failure=True):
                            changed = True
                            break
                        samples = [x for x in samples if x["t"] < t - GUARD_FAST_S]   # don't re-trigger at once
                self._sleep(SAMPLE_INTERVAL)
            if changed:
                continue
            self._guard_judge(g, samples)

    def _guard_judge(self, g, samples):
        """End of a full window on one rung: step down if it was bad, maybe step up if it was good."""
        c = self.state["config"]
        i = g["index"]
        rung = g["ladder"][i]
        if len(samples) < 3:
            self.log("Live guard: window had too few usable samples to judge; holding.")
            return
        r = self._summarize(samples, rung["freq"])
        bad = None
        if r["hash"] < 1:
            bad = "no hashrate"
        elif r["ratio"] is not None and r["ratio"] < c["min_hash_ratio"]:
            bad = f"hashrate {r['ratio']*100:.1f}% of expected (min {c['min_hash_ratio']*100:.0f}%)"
        elif r["err"] is not None and r["err"] > c["max_error_pct"]:
            bad = f"ASIC errors {r['err']:.2f}% > {c['max_error_pct']}%"
        elif r["reject_pct"] > c["max_reject_pct"]:
            bad = f"rejects {r['reject_pct']:.1f}% > {c['max_reject_pct']}%"
        if bad:
            self._guard_down(g, f"Window was unstable: {bad}.", failure=True)
            return
        h = c["guard_up_headroom"]
        vr = [x["vr"] for x in samples if x["vr"] is not None]
        tmax, vmax = r["temp_max"], (max(vr) if vr else None)
        if (tmax is not None and tmax > c["max_temp"] - h) or (vmax is not None and vmax > c["max_vr_temp"] - h):
            g["good"] = 0
            self.log(f"Live guard: window stable but warm (ASIC max {tmax}°C, VR max {vmax}°C); "
                     f"needs {h}°C of headroom to step up. Holding.")
            return
        g["good"] += 1
        if g["good"] == c["guard_up_windows"] and g["fails"].get(str(i)):
            g["fails"][str(i)] -= 1          # proven good again: forgive one earlier failure here
        j = i + 1
        if j > g["top"] or not self._guard_allowed(g, j):
            self.log(f"Live guard: stable window {g['good']} at the highest allowed tested setting. Holding.")
            self._save()
            return
        need = self._guard_need(g, j)
        if g["good"] >= need:
            self._guard_apply(g, j, "up", f"{g['good']} good windows in a row with temperature headroom.")
        else:
            self.log(f"Live guard: stable window {g['good']}/{need} before trying {g['ladder'][j]['freq']} MHz.")
            self._save()

    def _restore(self):
        """Put the miner back on its original settings. Safe to call from several threads."""
        orig = self.state.get("original")
        if not orig:
            return True
        with self.restore_lock:
            if not self._dirty:
                return True          # nothing was changed since the last restore
            dev = self.dev or Bitaxe(self.state["config"]["host"])
            for _ in range(5):
                try:
                    dev.set(orig["freq"], orig["volt"])
                    dev.restart()
                    self._dirty = False
                    self.log(f"Restored original settings ({orig['freq']} MHz / {orig['volt']} mV)")
                    return True
                except requests.RequestException:
                    time.sleep(3)
            return False

    @staticmethod
    def _device_summary(i):
        return {k: i.get(k) for k in ("hostname", "ASICModel", "deviceModel", "boardVersion",
                                       "version", "asicCount", "frequency", "coreVoltage",
                                       "expectedHashrate", "voltage", "nominalVoltage", "maxPower")}


def hottest_chip(info):
    """Multi-chip boards report a second chip as temp2 (0 or missing on single-chip boards).
    Safety checks must use the hottest chip, not just the first."""
    per_chip = info.get("asicTemps") if isinstance(info.get("asicTemps"), list) else []   # NerdAxe firmware
    temps = [t for t in (info.get("temp"), info.get("temp2"), *per_chip) if isinstance(t, (int, float)) and t > 0]
    return max(temps) if temps else info.get("temp")


def _clamp(key, v):
    s = SCHEMA_BY_KEY[key]
    return max(s["min"], min(s["max"], int(round(v))))


def _pos(v):
    return v if isinstance(v, (int, float)) and v > 0 else None


def build_profile(info, asic):
    """Factory profile for the detected miner, from (in order) the miner's own firmware report,
    the built-in Bitaxe table (by board version), the built-in NerdAxe table (by device model),
    or published vendor specs (e.g. Nexus S1). Returns None for miners it doesn't recognize."""
    dm = str((asic or {}).get("deviceModel") or info.get("deviceModel") or "")
    bv = str(info.get("boardVersion") or "")
    bitaxe = BOARDS["boards"].get(bv)
    nerd = EXTRA["nerd"].get(dm)
    spec = next((x for x in EXTRA["spec"] if re.search(x["match"], dm, re.I)), None)
    is_nerd = bool(nerd) or dm.lower().startswith("nerd") or "minPower" in info or bool(asic and "absMaxFrequency" in asic)
    brand = spec["brand"] if spec else "NerdAxe" if is_nerd else "Bitaxe" if bitaxe else ""
    entry = bitaxe if brand == "Bitaxe" else nerd if brand == "NerdAxe" else None

    if asic and asic.get("defaultFrequency"):
        p = {"source": "miner", "family": dm, "asic": asic.get("ASICModel") or info.get("ASICModel"),
             "chips": asic.get("asicCount") or info.get("asicCount"),
             "default_freq": asic.get("defaultFrequency"),
             "default_volt": asic.get("defaultVoltage") or info.get("defaultCoreVoltage"),
             "freq_options": [x for x in asic.get("frequencyOptions") or [] if isinstance(x, (int, float))],
             "volt_options": [x for x in asic.get("voltageOptions") or [] if isinstance(x, (int, float))],
             "abs_max_freq": _pos(asic.get("absMaxFrequency") or info.get("absMaxFrequency")),
             "abs_max_volt": _pos(asic.get("absMaxVoltage") or info.get("absMaxVoltage"))}
    elif entry:
        p = {"source": "built-in", "family": entry.get("family", dm), "asic": entry["asic"], "chips": entry["chips"],
             "default_freq": entry["default_freq"], "default_volt": entry["default_volt"],
             "freq_options": entry["freq_options"], "volt_options": entry["volt_options"],
             "abs_max_freq": entry.get("abs_max_freq"), "abs_max_volt": entry.get("abs_max_volt")}
    elif spec:
        p = {"source": "spec", "family": spec["name"], "asic": spec["asic"], "chips": spec["chips"],
             "default_freq": None, "default_volt": None, "freq_options": [], "volt_options": [],
             "abs_max_freq": None, "abs_max_volt": None}
    else:
        return None

    e = entry or {}
    vin_lo, vin_hi = _pos(info.get("minVoltage")), _pos(info.get("maxVoltage"))     # volts (NerdAxe firmware)
    if not (vin_lo and vin_hi) and e.get("vin"):
        vin_lo, vin_hi = e["vin"]
    nominal = (info.get("nominalVoltage") or e.get("nominal_v") or (spec or {}).get("nominal_v")
               or (12 if (vin_hi or 0) > 9 or (info.get("voltage") or 0) > 9000 else 5))
    gauge = _pos(info.get("maxPower")) or e.get("gauge_max_power")
    overheat = _pos(info.get("overheat_temp"))
    p.update(brand=brand, board_version=bv or None, nominal_v=nominal, gauge_max_power=gauge,
             overheat_temp=overheat, no_error_rate="errorPercentage" not in info,
             note=(spec or {}).get("note") or (e.get("note") if p["source"] == "built-in" else None),
             table_source={"Bitaxe": BOARDS.get("source"), "NerdAxe": EXTRA.get("nerd_source"),
                           "Nexus": EXTRA.get("spec_source")}.get(brand))

    # Starting limits from what the firmware defines:
    #  - start at the factory frequency/voltage (or the miner's current settings when unknown)
    #  - core voltage capped at the top of the firmware's own options, and never above the
    #    firmware's absolute maximum (enforced: the miner clamps anything higher at boot)
    #  - input window: the firmware's rated range when it reports one, else 95-110 % of nominal
    #  - power: maxPower is a UI gauge scale on both firmwares (not enforced), so it is only a floor
    #  - temperature: below the firmware's own overheat shutdown when it reports one
    df, top_f = p["default_freq"], max(p["freq_options"] or [0])
    ceiling = max(650, round(((df or 0) + 100) / 10) * 10, top_f + 50 if top_f else 0)
    if p["abs_max_freq"]:
        ceiling = min(ceiling, p["abs_max_freq"])
    vcap = max(p["volt_options"]) if p["volt_options"] else SCHEMA_BY_KEY["max_voltage"]["default"]
    if p["abs_max_volt"]:
        vcap = min(vcap, p["abs_max_volt"])
    if (spec or {}).get("max_voltage"):
        vcap = min(vcap, spec["max_voltage"])
    max_temp = (spec or {}).get("max_temp") or SCHEMA_BY_KEY["max_temp"]["default"]
    if overheat:
        max_temp = min(max_temp, overheat - 2)
    preset = {"start_freq": _clamp("start_freq", df or 0), "start_voltage": _clamp("start_voltage", p["default_volt"] or 0),
              "min_input_mv": _clamp("min_input_mv", (vin_lo * 1000) if vin_lo else nominal * 1000 * 0.95),
              "max_input_mv": _clamp("max_input_mv", (vin_hi * 1000) if vin_hi else nominal * 1000 * 1.10),
              "max_power": _clamp("max_power", max(100 if nominal >= 9 else 30, gauge or 0, (spec or {}).get("power_floor") or 0)),
              "max_freq": _clamp("max_freq", ceiling), "max_voltage": _clamp("max_voltage", vcap),
              "max_temp": _clamp("max_temp", max_temp)}
    p["preset"] = preset
    return p


def recommend(results, cfg):
    """Pick profiles from stable results."""
    stable = [r for r in results if r["status"] == "stable" and r.get("hash")]
    if not stable:
        return {}
    margin = cfg["margin_steps"] * cfg["freq_step"]
    out = {}

    def pack(r, label, why):
        return {"label": label, "why": why, "freq": r["freq"], "volt": r["volt"], "hash": r["hash"],
                "temp": r["temp"], "power": r["power"], "jth": r["jth"]}

    top = max(stable, key=lambda r: r["hash"])
    out["max_hashrate"] = pack(top, "Max hashrate", "Highest stable hashrate found. Little headroom.")
    eff = [r for r in stable if r.get("jth")]
    if eff:
        e = min(eff, key=lambda r: r["jth"])
        out["best_efficiency"] = pack(e, "Best efficiency", "Lowest joules per terahash.")

    # Safe daily: leave headroom below the failing frequency at the same voltage, stay cooler.
    failed = [r for r in results if r["status"] != "stable"]
    candidates = []
    for r in stable:
        fails_above = [x["freq"] for x in failed if x["volt"] == r["volt"] and x["freq"] > r["freq"]]
        # Headroom must be proven by test data, not assumed: either a failure sits far enough
        # above, or the same voltage was also tested stable at least `margin` higher.
        stable_above = any(x["volt"] == r["volt"] and x["freq"] >= r["freq"] + margin
                           and x["freq"] > r["freq"] for x in stable)
        if fails_above:
            headroom_ok = min(fails_above) - r["freq"] >= margin + cfg["freq_step"]
        else:
            headroom_ok = stable_above
        cool_ok = r["temp"] is None or r["temp"] <= cfg["max_temp"] - 4
        if headroom_ok and cool_ok:
            candidates.append(r)
    if candidates:
        best_eff = min((r["jth"] for r in candidates if r.get("jth")), default=None)
        pool = [r for r in candidates if not best_eff or (r.get("jth") and r["jth"] <= best_eff * 1.10)]
        pick = max(pool or candidates, key=lambda r: r["hash"])
        out["safe_daily"] = pack(pick, "Safe daily",
                                 "Stable with frequency headroom, ≥4°C under the temp limit, "
                                 "within 10% of best efficiency.")
    return out


def build_ladder(results, safe):
    """The guard's rungs: one per tested-stable frequency, the stable point with the highest
    hashrate at that frequency (ties: lower voltage). The safe daily point is always the rung at its
    frequency. Returns (rungs sorted by frequency, index of the safe daily rung)."""
    stable = [r for r in results if r["status"] == "stable" and r.get("hash")]
    by_f = {}
    for r in stable:
        cur = by_f.get(r["freq"])
        if cur is None or (r["hash"], -r["volt"]) > (cur["hash"], -cur["volt"]):
            by_f[r["freq"]] = r
    by_f[safe["freq"]] = next(r for r in stable if r["freq"] == safe["freq"] and r["volt"] == safe["volt"])
    ladder = [{"freq": r["freq"], "volt": r["volt"], "hash": r["hash"]} for _, r in sorted(by_f.items())]
    safe_i = next(i for i, r in enumerate(ladder) if r["freq"] == safe["freq"])
    return ladder, safe_i


tuner = Tuner()
app = Flask(__name__, static_folder="static", static_url_path="")


@app.errorhandler(Exception)
def unhandled(e):
    """Return the real reason as JSON instead of a bare 'Internal Server Error' page."""
    if isinstance(e, HTTPException):
        return e
    traceback.print_exc()
    return jsonify({"error": f"{type(e).__name__}: {e}"}), 500


def err(msg, code=400):
    return jsonify({"error": msg}), code


@app.get("/")
def index():
    return send_from_directory(app.static_folder, "index.html")


@app.get("/api/state")
def api_state():
    with tuner.lock:
        s = dict(tuner.state)
        s["recommendations"] = recommend(s["results"], s["config"])
        stable = [r for r in s["results"] if r["status"] == "stable" and r.get("hash")]
        s["best"] = max(stable, key=lambda r: r["hash"]) if stable else None
        s["server_time"] = time.time()
        s["storage_error"] = tuner.storage_error
        s["version"] = VERSION
        s["live_keys"] = LIVE_KEYS
        s["board_table"] = {"source": BOARDS.get("source"), "count": len(BOARDS.get("boards", {})),
                            "nerd_count": len(EXTRA.get("nerd", {})), "spec_count": len(EXTRA.get("spec", []))}
        s["epoch"] = tuner.epoch
        s["schema"] = SCHEMA
        s["test_mode"] = TEST_MODE
        s["active"] = tuner.active
        g = s.get("guard")
        s["guard_need"] = (tuner._guard_need(g, g["index"] + 1)
                           if g and g.get("ladder") and g["index"] < g["top"] else None)
        return jsonify(s)


@app.post("/api/detect")
def api_detect():
    try:
        host = clean_host((request.json or {}).get("host"))
        if not host:
            return err("Enter an IP address")
        dev = Bitaxe(host)
        info = dev.info()
        asic = dev.asic()
    except ValueError as e:
        return err(str(e))
    except requests.RequestException as e:
        return err(f"Couldn't reach {host}: {e}")
    summary = Tuner._device_summary(info)
    summary["profile"] = build_profile(info, asic)
    with tuner.lock:
        tuner.state["device"] = {k: v for k, v in summary.items() if k != "profile"}
        tuner.state["profile"] = summary["profile"]
    tuner._save()
    return jsonify(summary)


@app.post("/api/start")
def api_start():
    try:
        cfg = validate_config(request.json or {})
        tuner.start(cfg)
    except (ValueError, RuntimeError) as e:
        return err(str(e))
    return jsonify({"ok": True})


@app.post("/api/emergency")
def api_emergency():
    try:
        tuner.emergency()
    except RuntimeError as e:
        return err(str(e), 409)
    return jsonify({"ok": True})


@app.post("/api/restore")
def api_restore():
    try:
        orig = tuner.restore_original()
    except (RuntimeError, ValueError) as e:
        return err(str(e))
    except requests.RequestException as e:
        return err(f"Couldn't reach the miner: {e}")
    return jsonify({"ok": True, "freq": orig["freq"], "volt": orig["volt"]})


@app.get("/api/history")
def api_history():
    try:
        since = float(request.args.get("since", 0) or 0)
    except ValueError:
        since = 0.0
    with tuner.lock:
        if request.args.get("epoch") != str(tuner.epoch):
            since = 0.0
        samples = [x for x in tuner.history if x["t"] > since]
        return jsonify(epoch=tuner.epoch, samples=samples, markers=tuner.markers, now=time.time())


@app.post("/api/config")
def api_config():
    try:
        changes = tuner.update_live(request.json or {})
    except ValueError as e:
        return err(str(e))
    return jsonify({"ok": True, "changed": changes})


@app.post("/api/stop")
def api_stop():
    tuner.stop()
    return jsonify({"ok": True})


@app.post("/api/apply")
def api_apply():
    d = request.json or {}
    try:
        tuner.apply(d.get("host") or tuner.state["config"]["host"], int(d["freq"]), int(d["volt"]))
    except (ValueError, RuntimeError, KeyError, TypeError) as e:
        return err(str(e))
    except requests.RequestException as e:
        return err(f"Couldn't reach the miner: {e}")
    return jsonify({"ok": True})


@app.post("/api/reset")
def api_reset():
    if tuner.active:
        return err("Stop the run first")
    with tuner.lock:
        tuner.state.update(results=[], log=[], telemetry=None, guard=None)
        tuner.clear_history()
        tuner.state["run"] = {"status": "idle", "message": "", "phase": "", "current": None,
                              "started": None, "finished": None}
    tuner._save()
    return jsonify({"ok": True})


@app.get("/api/export")
def api_export():
    with tuner.lock:
        body = {"device": tuner.state["device"], "config": tuner.state["config"],
                "results": tuner.state["results"],
                "recommendations": recommend(tuner.state["results"], tuner.state["config"])}
    resp = jsonify(body)
    resp.headers["Content-Disposition"] = "attachment; filename=bitaxe-tuning-results.json"
    return resp


if __name__ == "__main__":
    from waitress import serve
    serve(app, host="0.0.0.0", port=int(os.environ.get("PORT", "8080")), threads=6)
