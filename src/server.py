"""Bitaxe Tuner: automatically finds safe overclock profiles for a Bitaxe (AxeOS).

Strategy
--------
Starting from the miner's current frequency/voltage, step frequency up. At each
point: apply settings, restart, warm up, then measure. A point is "stable" only if
hashrate tracks the expected value, hardware error rate and rejects stay low, and
no hard safety limit (temperature, VR temperature, power, input voltage) is hit.
When a voltage's ceiling is found, voltage is raised one step and the sweep
continues from the best stable frequency. The miner's original settings are
always restored when the run ends, is stopped, fails, or the container restarts.
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

DATA_DIR = os.environ.get("DATA_DIR", "/data")
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
    dict(key="max_power", label="Max power", unit="W", default=30, min=5, max=120,
         help="Hard stop. Set to what your PSU/model can safely supply."),
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
]
SCHEMA_BY_KEY = {s["key"]: s for s in SCHEMA}
DEFAULTS = {s["key"]: s["default"] for s in SCHEMA}
DEFAULTS["host"] = ""

if TEST_MODE:  # allow short timings in the simulator
    SCHEMA_BY_KEY["warmup_s"]["min"] = 0
    SCHEMA_BY_KEY["measure_s"]["min"] = 1

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
        s = SCHEMA_BY_KEY.get(k)
        if not s:
            continue
        try:
            n = float(v)
        except (TypeError, ValueError):
            raise ValueError(f"{s['label']} must be a number")
        if not (s["min"] <= n <= s["max"]):
            raise ValueError(f"{s['label']} must be between {s['min']} and {s['max']}")
        cfg[k] = int(n) if float(s["default"]).is_integer() and s["key"] != "min_hash_ratio" else n
    return cfg


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
            "original": None,
            "results": [],
            "telemetry": None,
            "log": [],
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
        """If we died mid-run, the miner may be left on a test setting: restore it."""
        run = self.state["run"]
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
            "temp": i.get("temp"), "vr": i.get("vrTemp"), "power": i.get("power"),
            "vin": i.get("voltage"), "err": i.get("errorPercentage"),
            "acc": i.get("sharesAccepted") or 0, "rej": i.get("sharesRejected") or 0,
            "fan": i.get("fanspeed") if i.get("fanspeed") is not None else i.get("fanrpm"),
            "freq": i.get("frequency"), "volt": i.get("coreVoltage"),
            "expected": i.get("expectedHashrate"),
            "cores": (i.get("smallCoreCount") or 0) * (i.get("asicCount") or 0),
        }

    def _hard_limit(self, s):
        c = self.state["config"]
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
                "It rejected or clamped the setting.")

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
                if best_here is None:
                    message = ("The starting point was unstable; lower the start frequency/voltage."
                               if first else "No stable gain at the higher voltage; sweep complete.")
                    break
                first = False
                if hit_hard:
                    message = f"Stopped at a safety limit ({hit_hard['reason']})."
                    break
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
        except Aborted:
            status, message = "stopped", "Stopped by you."
        except Exception as e:  # noqa: BLE001
            if self.stop_ev.is_set():      # errors caused by our own emergency restore are not errors
                status, message = "stopped", "Stopped by you."
            else:
                status, message = "error", str(e)
        finally:
            restored = self._restore()
            if not restored:
                status = "error"
                message += " WARNING: could not restore the original settings. Check the miner in AxeOS!"
            elif self.emergency_flag and status == "stopped":
                message = "Emergency stop: original settings restored."
            self._save_history(force=True)
            self.log(message)
            self._set_run(status=status, message=message, phase="", current=None, finished=now())

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
                                       "expectedHashrate", "voltage")}


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
        s["epoch"] = tuner.epoch
        s["schema"] = SCHEMA
        s["test_mode"] = TEST_MODE
        s["active"] = tuner.active
        return jsonify(s)


@app.post("/api/detect")
def api_detect():
    try:
        host = clean_host((request.json or {}).get("host"))
        if not host:
            return err("Enter an IP address")
        info = Bitaxe(host).info()
    except ValueError as e:
        return err(str(e))
    except requests.RequestException as e:
        return err(f"Couldn't reach {host}: {e}")
    return jsonify(Tuner._device_summary(info))


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
        tuner.state.update(results=[], log=[], telemetry=None)
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
