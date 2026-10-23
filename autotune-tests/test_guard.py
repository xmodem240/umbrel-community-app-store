"""Tests for the Bitaxe Autotuner live guard, run against the simulated miner (mock_bitaxe.py).

    python -m unittest discover -s autotune-tests -p "test_*.py" -v

Timings are shortened (5 s samples become 0.05 s, 15 min windows become 1 s) so the whole
sweep + guard runs in seconds. The simulated board is a Bitaxe Ultra (485 MHz / 1200 mV factory),
whose stable ceiling is 500 MHz at 1200 mV and rises 3 MHz per mV.
"""
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = os.path.join(HERE, "..", "autotune-src")
DATA = tempfile.mkdtemp(prefix="autotune-test-")
FAST_ENV = {"TUNER_TEST_MODE": "1", "SAMPLE_INTERVAL": "0.05", "RESTART_WAIT": "0.1",
            "GUARD_SETTLE_LIVE": "0.1", "GUARD_CONFIRM_S": "1", "GUARD_FAST_S": "0.5"}
os.environ.update(FAST_ENV, DATA_DIR=DATA)
sys.path.insert(0, SRC)
import server  # noqa: E402

F0, V0 = 485, 1200
BASE_CFG = dict(warmup_s=0, measure_s=1, retries=0, max_voltage=1220, max_freq=560,
                guard_window_s=1, guard_up_windows=2)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def http(method, url, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method, headers={"content-type": "application/json"})
    with urllib.request.urlopen(req, timeout=5) as r:
        return json.loads(r.read() or b"{}")


def wait_until(pred, timeout=60, what="condition"):
    end = time.time() + timeout
    while time.time() < end:
        try:
            v = pred()
            if v:
                return v
        except Exception:  # noqa: BLE001  (miner rebooting, state not there yet)
            pass
        time.sleep(0.05)
    raise AssertionError(f"timed out waiting for {what}")


class Mock:
    def __init__(self, live="1"):
        self.port = free_port()
        env = dict(os.environ, MOCK_LIVE=live, MOCK_BOOT_S="0.05")
        self.p = subprocess.Popen([sys.executable, os.path.join(HERE, "mock_bitaxe.py"), str(self.port)],
                                  env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.host = f"127.0.0.1:{self.port}"
        wait_until(lambda: self.stats(), 20, "mock miner")

    def stats(self):
        return http("GET", f"http://{self.host}/mock/stats")

    def set(self, **kw):
        http("POST", f"http://{self.host}/mock/set", kw)

    def close(self):
        self.p.kill()
        self.p.wait()


def clean_data():
    for f in os.listdir(DATA):
        os.remove(os.path.join(DATA, f))


class GuardCase(unittest.TestCase):
    live = "1"
    extra = {}

    def setUp(self):
        clean_data()
        self.mock = Mock(self.live)
        self.t = server.Tuner()
        server.tuner = self.t            # so /api/state (via the Flask test client) reports this run
        self.client = server.app.test_client()
        self.t.start(server.validate_config(dict(BASE_CFG, host=self.mock.host, **self.extra)))

    def tearDown(self):
        if self.t.active:
            self.t.stop()
            wait_until(lambda: not self.t.active, 30, "run to end")
        self.mock.close()

    # helpers
    def g(self):
        return self.t.state["guard"]

    def wait_guard(self):
        wait_until(lambda: self.g() and self.g()["active"] and self.t.state["run"]["phase"] == "guard",
                   120, "live guard to start")
        return self.g()

    def api_state(self):
        return self.client.get("/api/state").get_json()

    def rung(self):
        g = self.g()
        return g["ladder"][g["index"]]

    def wait_top(self):
        self.wait_guard()
        wait_until(lambda: self.g()["index"] == self.g()["top"], 60, "guard to reach the ceiling")

    def events_after(self, n):
        return self.g()["events"][n:]

    def stop_and_check_restored(self):
        self.t.stop()
        wait_until(lambda: not self.t.active, 30, "run to end")
        st = self.mock.stats()
        self.assertEqual((st["freq"], st["volt"]), (F0, V0), "original settings must be restored")
        self.assertEqual(self.t.state["run"]["status"], "stopped")


class TestLadder(unittest.TestCase):
    def test_one_rung_per_frequency_and_safe_daily_kept(self):
        R = lambda f, v, h, st="stable": {"freq": f, "volt": v, "hash": h, "status": st}
        results = [R(500, 1200, 600), R(510, 1200, 610), R(520, 1200, 0, "unstable"),
                   R(520, 1210, 625), R(510, 1210, 612), R(530, 1210, 0, "thermal")]
        ladder, i = server.build_ladder(results, {"freq": 510, "volt": 1200})
        self.assertEqual([(r["freq"], r["volt"]) for r in ladder], [(500, 1200), (510, 1200), (520, 1210)])
        self.assertEqual(i, 1, "the safe daily point is its own rung even if another voltage hashed more")

    def test_failed_points_never_become_rungs(self):
        R = lambda f, st: {"freq": f, "volt": 1200, "hash": 600, "status": st}
        ladder, _ = server.build_ladder([R(500, "stable"), R(510, "unstable"), R(520, "power")],
                                        {"freq": 500, "volt": 1200})
        self.assertEqual([r["freq"] for r in ladder], [500])


class TestLiveGuard(GuardCase):
    def test_climbs_to_ceiling_live_without_restarts_then_stop_restores(self):
        g = self.wait_guard()
        safe = g["ladder"][g["safe"]]
        self.assertEqual(self.rung(), safe, "guard starts on safe daily")
        restarts = self.mock.stats()["restarts"]
        self.wait_top()
        self.assertEqual(self.g()["mode"], "live")
        self.assertEqual(self.mock.stats()["restarts"], restarts, "live mode must not restart the miner")
        top = self.g()["ladder"][self.g()["top"]]
        best_stable = max(r["freq"] for r in self.t.state["results"] if r["status"] == "stable")
        self.assertEqual(top["freq"], best_stable, "ceiling is the highest tested-stable frequency")
        time.sleep(3)   # several more windows: must hold, never go above the ceiling
        self.assertEqual(self.g()["index"], self.g()["top"])
        self.assertEqual(self.mock.stats()["afreq"], top["freq"])
        self.stop_and_check_restored()

    def test_heat_steps_down_without_penalty_and_recovers(self):
        self.wait_top()
        n = len(self.g()["events"])
        self.mock.set(ambient=9)        # within 4 C of the 66 C limit, not over it
        ev = wait_until(lambda: [e for e in self.events_after(n) if e["kind"] == "down"], 20, "heat step-down")
        self.assertIn("within 4°C", ev[0]["reason"])
        self.mock.set(ambient=0)
        wait_until(lambda: self.g()["index"] == self.g()["top"], 60, "climb back after cooling")
        self.assertFalse(any(self.g()["fails"].values()), "heat is not a stability failure")
        self.stop_and_check_restored()

    def test_errors_step_down_fast_and_double_the_wait_to_return(self):
        self.wait_top()
        top = self.g()["top"]
        n = len(self.g()["events"])
        self.mock.set(ceiling=-10)      # the top rung now throws errors
        ev = wait_until(lambda: [e for e in self.events_after(n) if e["kind"] == "down"], 20, "error step-down")
        self.assertIn("ASIC errors", ev[0]["reason"])
        self.assertEqual(self.g()["index"], top - 1)
        self.assertEqual(self.g()["fails"].get(str(top)), 1)
        self.assertEqual(self.api_state()["guard_need"], 4, "one failure doubles the 2 windows needed")
        self.mock.set(ceiling=0)
        wait_until(lambda: self.g()["index"] == top, 60, "return after more good windows")
        ups = [e for e in self.events_after(n) if e["kind"] == "up"]
        self.assertIn("4 good windows", ups[-1]["reason"])
        self.stop_and_check_restored()

    def test_firmware_throttle_is_respected(self):
        self.wait_top()
        n = len(self.g()["events"])
        self.mock.set(throttle_to=520)
        ev = wait_until(lambda: self.events_after(n), 20, "throttle step-down")
        self.assertIn("Firmware throttled", ev[0]["reason"])
        self.assertLessEqual(ev[0]["to"]["freq"], 520, "never run above what the firmware throttled to")
        self.stop_and_check_restored()

    def test_lowering_max_frequency_mid_run_moves_down_and_caps(self):
        self.wait_top()
        self.t.update_live({"max_freq": 530})
        wait_until(lambda: self.rung()["freq"] <= 530, 20, "move under new max")
        self.assertEqual(self.rung()["freq"], 525)
        time.sleep(3)
        self.assertLessEqual(self.rung()["freq"], 530, "must not step above the lowered max")
        self.stop_and_check_restored()

    def test_hard_limit_at_lowest_setting_ends_and_restores(self):
        self.wait_guard()
        self.t.update_live({"max_freq": 485})
        wait_until(lambda: self.g()["index"] == 0, 20, "move to the lowest setting")
        self.mock.set(ambient=20)
        wait_until(lambda: not self.t.active, 30, "run to end")
        self.assertEqual(self.t.state["run"]["status"], "error")
        self.assertIn("lowest tested setting", self.t.state["run"]["message"])
        st = self.mock.stats()
        self.assertEqual((st["freq"], st["volt"]), (F0, V0))

    def test_firmware_overheat_ends_and_restores(self):
        self.wait_guard()
        self.mock.set(overheat=1)
        wait_until(lambda: not self.t.active, 30, "run to end")
        self.assertEqual(self.t.state["run"]["status"], "error")
        self.assertIn("overheat", self.t.state["run"]["message"])
        st = self.mock.stats()
        self.assertEqual((st["freq"], st["volt"]), (F0, V0))

    def test_emergency_stop_during_guard_restores(self):
        self.wait_guard()
        self.t.emergency()
        wait_until(lambda: not self.t.active, 30, "run to end")
        st = self.mock.stats()
        self.assertEqual((st["freq"], st["volt"]), (F0, V0))
        self.assertIn("Emergency stop", self.t.state["run"]["message"])


class TestHardLimitToSafe(GuardCase):
    # 2-step safety margin and one more voltage step put safe daily two settings below the ceiling
    extra = {"margin_steps": 2, "max_voltage": 1230, "max_freq": 600}

    def test_hard_limit_jumps_straight_to_safe_daily(self):
        self.wait_top()
        g = self.g()
        safe = g["ladder"][g["safe"]]
        self.assertGreaterEqual(g["top"] - g["safe"], 2,
                                "needs room above safe daily, or 'one step down' and 'safe daily' are the same")
        n = len(g["events"])
        self.mock.set(ambient=14)       # over the 66 C hard limit
        ev = wait_until(lambda: self.events_after(n), 20, "hard-limit step-down")
        self.mock.set(ambient=0)
        self.assertEqual(ev[0]["kind"], "down")
        self.assertIn("Hard limit", ev[0]["reason"])
        self.assertEqual(ev[0]["to"], {"freq": safe["freq"], "volt": safe["volt"]})
        self.stop_and_check_restored()


class TestRestartFallback(GuardCase):
    live = "0"   # firmware saves the setting but only applies it at restart

    def test_falls_back_to_restart_mode_and_steps_up_half_as_often(self):
        g = self.wait_guard()
        self.assertEqual(g["mode"], "restart")
        self.assertEqual(self.mock.stats()["afreq"], self.rung()["freq"], "the restart applied the setting")
        self.assertEqual(self.api_state()["guard_need"], 4)
        r0 = self.mock.stats()["restarts"]
        wait_until(lambda: [e for e in g["events"] if e["kind"] == "up"], 60, "a step up")
        self.assertGreater(self.mock.stats()["restarts"], r0, "restart mode restarts per change")
        self.stop_and_check_restored()


class TestNoLiveField(GuardCase):
    live = "nofield"

    def test_firmware_without_actual_frequency_uses_restart_mode(self):
        g = self.wait_guard()
        self.assertEqual(g["mode"], "restart")
        self.assertTrue(any("doesn't report its live frequency" in l for l in self.t.state["log"]))
        self.stop_and_check_restored()


class TestNoSafeDaily(GuardCase):
    extra = {"max_freq": 485}   # one stable point, no proven headroom -> no safe daily

    def test_sweep_without_safe_daily_ends_and_restores(self):
        wait_until(lambda: not self.t.active and self.t.state["run"]["status"] != "running", 60, "run to end")
        self.assertEqual(self.t.state["run"]["status"], "done")
        self.assertIn("live guard did not start", self.t.state["run"]["message"])
        self.assertIsNone(self.t.state["guard"])
        st = self.mock.stats()
        self.assertEqual((st["freq"], st["volt"]), (F0, V0))


class TestCrashResume(unittest.TestCase):
    """kill -9 the real app process mid-guard, start it again: it must resume guarding from safe
    daily, and Stop must still restore the original settings."""

    def test_resume_after_kill(self):
        mock = Mock("1")
        data = tempfile.mkdtemp(prefix="autotune-crash-")
        port = free_port()
        env = dict(os.environ, DATA_DIR=data, PORT=str(port))
        base = f"http://127.0.0.1:{port}"

        def launch():
            p = subprocess.Popen([sys.executable, os.path.join(SRC, "server.py")], env=env,
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            wait_until(lambda: http("GET", base + "/api/state"), 20, "app")
            return p
        p = launch()
        try:
            http("POST", base + "/api/start", dict(BASE_CFG, host=mock.host))
            st = wait_until(lambda: (lambda s: s if s.get("guard") and s["guard"]["active"]
                                     and s["run"]["phase"] == "guard" else None)(http("GET", base + "/api/state")),
                            120, "guard via API")
            safe = st["guard"]["ladder"][st["guard"]["safe"]]
            wait_until(lambda: http("GET", base + "/api/state")["guard"]["index"] > st["guard"]["safe"],
                       30, "a step above safe daily")
            p.send_signal(signal.SIGKILL)
            p.wait()
            p = launch()
            s2 = http("GET", base + "/api/state")
            self.assertTrue(s2["active"], "guard thread resumed")
            self.assertTrue(any("resuming from the safe daily" in l for l in s2["log"]))
            starts = wait_until(lambda: [e for e in http("GET", base + "/api/state")["guard"]["events"]
                                         if e["kind"] == "start" and "Resumed" in e["reason"]], 20, "resume event")
            self.assertEqual(starts[-1]["to"]["freq"], safe["freq"])
            self.assertIn("Resumed", starts[-1]["reason"])
            http("POST", base + "/api/stop", {})
            wait_until(lambda: not http("GET", base + "/api/state")["active"], 30, "stop")
            m = mock.stats()
            self.assertEqual((m["freq"], m["volt"]), (F0, V0))
        finally:
            p.kill()
            p.wait()
            mock.close()
            shutil.rmtree(data, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
