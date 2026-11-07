#!/usr/bin/env python3
"""
AxePool - miner API poller tests.

Normalization is checked against two real responses captured from the
maintainer's miners (fixtures/, personal identifiers redacted): a Bitaxe Naja
Duo on AxeOS v2.15.4 and a NerdQaxe++ on v1.1.0.1 that was shut down. The rest
covers address safety, the HTTP client (redirects, size cap, timeouts) against
a real local server, backoff and discovery, and the settings/API wiring.

Offline, standard library only:   cd server && python3 test_miner_poller.py
"""
import asyncio
import copy
import http.server
import json
import os
import sys
import tempfile
import threading
import time

DATA_DIR = tempfile.mkdtemp(prefix="axepool-poller-test-")
os.environ["APP_DATA_DIR"] = DATA_DIR
for k in list(os.environ):
    if k.startswith(("SOLOBCH_", "BCHN_")):
        del os.environ[k]
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import logging                                   # noqa: E402
import miner_poller as mp                        # noqa: E402
from miner_poller import (MinerPoller, normalize, parse_target,   # noqa: E402
                          parse_target_list, fetch_info)
import stratum                                   # noqa: E402
import status                                    # noqa: E402
import config                                    # noqa: E402

logging.getLogger("solobch").setLevel(logging.CRITICAL)
ok = fail = 0


def check(label, cond):
    global ok, fail
    if cond:
        ok += 1
        print(f"PASS  {label}")
    else:
        fail += 1
        print(f"FAIL  {label}")


def fixture(name):
    with open(os.path.join(HERE, "fixtures", name)) as f:
        return json.load(f)


AXE = fixture("axeos_v2.15.4_naja_duo.json")
NERD = fixture("nerdqaxe_v1.1.0.1_nerdqaxepp_shutdown.json")


# --------------------------------------------------------------------------- #
def test_normalize():
    a = normalize(AXE, "192.0.2.10")
    check("AxeOS: recognised as axeos firmware", a["firmware"] == "axeos")
    check("AxeOS: hashrate kept in GH/s (7348.27)", round(a["hashrate_ghs"], 2) == 7348.27)
    check("AxeOS: 1m/10m/1h hashrates read",
          a["hashrate_1m_ghs"] and a["hashrate_10m_ghs"] and a["hashrate_1h_ghs"])
    check("AxeOS: expected hashrate from firmware, not derived",
          a["expected_ghs"] == 6953.65 and not a["expected_derived"])
    check("AxeOS: mV -> V and mA -> A (12.109 V, 37.75 A)",
          a["voltage_v"] == 12.109 and a["current_a"] == 37.75)
    check("AxeOS: chip temp is the hotter of temp/temp2 (49.95)",
          round(a["chip_temp_c"], 2) == 49.95)
    check("AxeOS: state mining, model from board version",
          a["state"] == "mining" and a["model"] == "board 1201" and a["version"] == "v2.15.4")
    check("AxeOS: pool difficulty and error % read",
          a["pool_difficulty"] == 4526 and round(a["error_pct"], 3) == 0.175)

    n = normalize(NERD, "192.0.2.11")
    check("NerdQaxe: recognised as nerdqaxe firmware", n["firmware"] == "nerdqaxe")
    check("NerdQaxe: shut down -> state 'stopped' (not idle/offline)", n["state"] == "stopped")
    check("NerdQaxe: hashrate 0 while stopped", n["hashrate_ghs"] == 0)
    check("NerdQaxe: expected hashrate derived from 605 MHz x 2040 cores x 4 chips",
          n["expected_ghs"] == 4936.8 and n["expected_derived"])
    check("NerdQaxe: zero per-chip temps ignored, hottest real reading kept (28.875)",
          n["chip_temp_c"] == 28.875)
    check("NerdQaxe: model from deviceModel", n["model"] == "NerdQAxe++")

    # Same derivation reproduces AxeOS's own expected figure (cross-check).
    a2 = dict(AXE)
    a2.pop("expectedHashrate")
    a2["asicCount"] = 2
    check("derived expected matches AxeOS's own number within 0.1%",
          abs(normalize(a2)["expected_ghs"] - 6953.65) / 6953.65 < 0.001)

    # Personal data never leaks into the record.
    raw = copy.deepcopy(AXE)
    raw.update(ssid="SECRETNET", macAddr="DE:AD:BE:EF:00:01", stratumPassword="hunter2",
               fallbackStratumUser="bitcoincash:qother.acct", wifiPass="pw123")
    blob = json.dumps(normalize(raw))
    check("no SSID, MAC, passwords or fallback account in the record",
          not any(x in blob for x in ("SECRETNET", "DE:AD:BE", "hunter2", "qother", "pw123")))
    raw["isUsingFallbackStratum"] = 1
    check("on the fallback pool, its username is the one reported (and flagged)",
          normalize(raw)["stratum_user"] == "bitcoincash:qother.acct"
          and normalize(raw)["on_fallback_pool"])

    # Garbage in, no crash out.
    bad = dict(AXE, hashRate="abc", temp=float("nan"), temp2=-5, power=1e9,
               voltage="12000", vrTemp=None, fanspeed=250)
    b = normalize(bad)
    check("garbage fields become None, valid strings still parse",
          b["hashrate_ghs"] is None and b["power_w"] is None and b["fan_pct"] is None
          and b["voltage_v"] == 12.0 and b["chip_temp_c"] is None and b["state"] == "idle")
    check("non-dict / non-miner JSON -> None",
          normalize([1, 2]) is None and normalize({"hello": 1}) is None and normalize(None) is None)
    check("overheat and paused states",
          normalize(dict(AXE, overheat_mode=1))["state"] == "overheat"
          and normalize(dict(AXE, miningPaused=True))["state"] == "paused")
    check("bool is not a number (True -> None, not 1.0)",
          normalize(dict(AXE, power=True))["power_w"] is None)


def test_addresses():
    check("plain private IPv4 accepted", parse_target("192.168.50.74") == ("192.168.50.74", 80))
    check("port accepted", parse_target("10.0.0.5:8080") == ("10.0.0.5", 8080))
    check("172.16/12 accepted", parse_target("172.20.1.1")[0] == "172.20.1.1")
    for bad in ("8.8.8.8", "127.0.0.1", "169.254.1.1", "0.0.0.0", "172.32.0.1",
                "bitaxe.local", "http://192.168.1.2", "192.168.1.2/../x", "192.168.1.2:0",
                "192.168.1.2:70000", "::1", "fd00::1", "192.168.1", "224.0.0.1",
                "192.168.1.2:80@evil.com", ""):
        try:
            parse_target(bad)
            check(f"rejected {bad!r}", False)
        except ValueError:
            check(f"rejected {bad!r}", True)
    check("list: commas/spaces/newlines, duplicates removed",
          parse_target_list("192.168.1.2, 192.168.1.3\n192.168.1.2") ==
          [("192.168.1.2", 80), ("192.168.1.3", 80)])
    check("list: empty -> []", parse_target_list("") == [] and parse_target_list(None) == [])
    try:
        parse_target_list(" ".join(f"10.0.0.{i}" for i in range(1, 40)))
        check("list capped at 32 entries", False)
    except ValueError:
        check("list capped at 32 entries", True)


# --------------------------------------------------------------------------- #
class Clock:
    def __init__(self):
        self.t = 1000.0

    def __call__(self):
        return self.t


class Conn:
    def __init__(self, ip, user, internal=False, authorized=True):
        self.peer = (ip, 50000)
        self.raw_user = user
        self.internal = internal
        self.authorized = authorized


def test_poller():
    clock = Clock()
    replies = {"192.168.1.10": AXE, "192.168.1.11": NERD}
    calls = []

    def fake_fetch(ip, port):
        calls.append(ip)
        r = replies.get(ip)
        if isinstance(r, Exception):
            raise r
        if r is None:
            raise TimeoutError("timed out")
        return r

    p = MinerPoller(fetch=fake_fetch, now=clock, wall=clock)
    p.note_connections([Conn("192.168.1.10", "bitcoincash:qa.rig1"),
                        Conn("192.168.1.11", "bitcoincash:qa.nerd"),
                        Conn("8.8.8.8", "bitcoincash:qa.public"),
                        Conn("127.0.0.1", "diag", internal=True),
                        Conn("192.168.1.12", None, authorized=False)])
    check("discovery: only authorized, non-internal, private addresses",
          set(p.targets) == {"192.168.1.10:80", "192.168.1.11:80"})
    asyncio.run(p.poll_due())
    check("first poll reads both", sorted(calls) == ["192.168.1.10", "192.168.1.11"])
    snap = p.snapshot(["bitcoincash:qa.rig1", "bitcoincash:qa.nerd"])
    st = {r["address"]: r for r in snap["miners"]}
    check("states: mining and stopped",
          st["192.168.1.10:80"]["status"] == "mining" and st["192.168.1.11:80"]["status"] == "stopped")
    check("linked to pool identity by connection address",
          st["192.168.1.10:80"]["linked_identity"] == "bitcoincash:qa.rig1")
    calls.clear()
    asyncio.run(p.poll_due())
    check("not re-read before POLL_INTERVAL (Umbrel/miner load)", calls == [])
    clock.t += mp.POLL_INTERVAL
    asyncio.run(p.poll_due())
    check("re-read after POLL_INTERVAL", len(calls) == 2)

    # A miner goes dark: backoff, then offline; it recovers.
    replies["192.168.1.10"] = OSError("connection refused")
    gaps = []
    for _ in range(5):
        clock.t = max(t.next_due for t in p.targets.values()) + 0.01
        before = clock.t
        asyncio.run(p.poll_due())
        gaps.append(p.targets["192.168.1.10:80"].next_due - before)
    t10 = p.targets["192.168.1.10:80"]
    check("backoff grows and is capped at 2 minutes",
          gaps == sorted(gaps) and gaps[-1] <= mp.MAX_BACKOFF and gaps[0] > mp.POLL_INTERVAL)
    check("offline after repeated failures, error kept",
          t10.status() == "offline" and "refused" in t10.last_error)
    check("offline miner not linked into graphs", "bitcoincash:qa.rig1" not in
          p.link(["bitcoincash:qa.rig1", "bitcoincash:qa.nerd"]))
    replies["192.168.1.10"] = AXE
    clock.t = t10.next_due
    asyncio.run(p.poll_due())
    check("recovers: failures reset, mining again",
          t10.failures == 0 and t10.status() == "mining")

    # Not a miner (e.g. a router web page answering on port 80).
    replies["192.168.1.11"] = {"router": "hello"}
    clock.t = p.targets["192.168.1.11:80"].next_due
    asyncio.run(p.poll_due())
    check("non-miner JSON counted as a failure with a clear error",
          "not a miner" in p.targets["192.168.1.11:80"].last_error)

    # Auto address that never answered (e.g. Docker gateway) is hidden; manual is shown.
    p.note_connections([Conn("192.168.1.10", "bitcoincash:qa.rig1"),
                        Conn("172.17.0.1", "bitcoincash:qa.ghost")])
    p.set_manual([("192.168.1.99", 80)])
    clock.t += 1000
    asyncio.run(p.poll_due())
    addrs = [r["address"] for r in p.snapshot()["miners"]]
    check("auto address that never answered is hidden",
          "172.17.0.1:80" not in addrs)
    check("manual address shown even while failing", "192.168.1.99:80" in addrs)

    # Two rigs behind one address: no guessing by address; stratum_user decides.
    p2 = MinerPoller(fetch=lambda ip, port: dict(AXE, stratumUser="bitcoincash:qb.two"),
                     now=clock, wall=clock)
    p2.note_connections([Conn("192.168.1.50", "bitcoincash:qb.one"),
                         Conn("192.168.1.50", "bitcoincash:qb.two")])
    asyncio.run(p2.poll_due())
    check("shared address: linked by the miner's own Stratum username",
          list(p2.link(["bitcoincash:qb.one", "bitcoincash:qb.two"])) == ["bitcoincash:qb.two"])

    # Forget an auto address after an hour without a connection.
    p2.note_connections([])
    clock.t += mp.FORGET_AUTO_AFTER + 1
    p2.note_connections([])
    check("auto address forgotten after an hour unseen", p2.targets == {})

    # Manual list replacement keeps auto targets.
    p.set_manual([])
    check("clearing manual list keeps auto-discovered miners",
          "192.168.1.99:80" not in p.targets and "192.168.1.10:80" in p.targets)

    # Cap.
    p3 = MinerPoller(fetch=fake_fetch, now=clock, wall=clock)
    p3.note_connections([Conn(f"10.0.{i // 250}.{i % 250 + 1}", f"u{i}") for i in range(200)])
    check("target list capped", len(p3.targets) == mp.MAX_TARGETS)

    # Disabled: nothing is read.
    calls.clear()
    p.enabled = False
    clock.t += 10000
    check("disabled poller reads nothing", asyncio.run(p.poll_due()) == 0 and calls == [])
    for x in (p, p2, p3):
        x.close()


def test_concurrency():
    """Slow miners are read in parallel off the event loop."""
    def slow(ip, port):
        time.sleep(0.5)
        return AXE
    p = MinerPoller(fetch=slow)
    p.set_manual([(f"192.168.7.{i}", 80) for i in range(1, 5)])

    async def run():
        gaps, last = [], time.perf_counter()
        stop = False

        async def ticker():
            nonlocal last
            while not stop:
                await asyncio.sleep(0.02)
                now = time.perf_counter()
                gaps.append(now - last)
                last = now
        tk = asyncio.create_task(ticker())
        t0 = time.perf_counter()
        n = await p.poll_due()
        took = time.perf_counter() - t0
        stop = True
        await tk
        return n, took, max(gaps)
    n, took, worst_gap = asyncio.run(run())
    check(f"4 slow (0.5 s) miners read in parallel ({took:.2f} s)", n == 4 and took < 1.0)
    check(f"event loop never blocked (worst tick gap {worst_gap * 1000:.0f} ms)",
          worst_gap < 0.2)
    p.close()


# --------------------------------------------------------------------------- #
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        mode = self.server.mode
        self.server.seen_origin = self.headers.get("Origin")
        if mode == "redirect" and "followed" not in self.path:
            # Points at a URL that WOULD succeed, so following it would pass
            # unnoticed unless redirects are really refused.
            self.send_response(302)
            self.send_header("Location", mp.API_PATH + "?followed=1")
            self.end_headers()
            return
        if mode == "404":
            self.send_error(404)
            return
        if mode == "slow":
            time.sleep(mp.HTTP_TIMEOUT + 1.5)
        # Oversize body is still valid JSON, so only the size cap can refuse it.
        body = json.dumps(dict(AXE, pad="x" * (mp.MAX_RESPONSE + 10)) if mode == "huge"
                          else AXE).encode()
        if not self.path.startswith(mp.API_PATH):
            self.send_error(400)
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def test_http_client():
    srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    srv.daemon_threads = True
    port = srv.server_address[1]
    threading.Thread(target=srv.serve_forever, daemon=True).start()

    def mode(m):
        srv.mode = m

    mode("ok")
    r = fetch_info("127.0.0.1", port)
    check("real HTTP: fixed path fetched and parsed", r.get("hashRate") == AXE["hashRate"])
    check("no Origin header sent (firmware LAN check uses source IP)", srv.seen_origin is None)
    for m, label, why in (("redirect", "redirect refused, not followed", None),
                          ("404", "HTTP error raises", None),
                          ("huge", "oversize response refused by the size cap", "too large")):
        mode(m)
        try:
            fetch_info("127.0.0.1", port)
            check(label, False)
        except Exception as e:
            check(label, why is None or why in str(e))
    mode("slow")
    t0 = time.perf_counter()
    try:
        fetch_info("127.0.0.1", port)
        check("slow miner times out", False)
    except Exception:
        took = time.perf_counter() - t0
        check(f"slow miner times out near HTTP_TIMEOUT ({took:.1f} s)",
              took < mp.HTTP_TIMEOUT + 1.0)
    srv.shutdown()


# --------------------------------------------------------------------------- #
class SConn:
    def __init__(self, user):
        self.raw_user = user
        self.internal = False
        self.authorized = True
        self.difficulty = 1000.0
        self.rejected = 0
        self._shares = [(5.0, 100.0)]

    def window_hashrate(self, w):
        return 7e12


def test_sample_with_devices():
    import weakref
    a, b = SConn("bitcoincash:qa.rig1"), SConn("bitcoincash:qa.rig2")
    c = SConn("bitcoincash:qa.nerd")
    devices = {"bitcoincash:qa.rig1": normalize(AXE), "bitcoincash:qa.nerd": normalize(NERD)}
    pool, miners = stratum.collect_sample([a, b, c], 0.0, weakref.WeakKeyDictionary(), 10.0,
                                          devices=devices)
    m1 = miners["bitcoincash:qa.rig1"]
    check("polled miner row carries reported hashrate, power, temps",
          round(m1["reported_ghs"], 2) == 7348.27 and round(m1["power_w"], 1) == 81.2
          and m1["vr_temp_c"] == 50)
    check("unpolled miner row has no reported fields", "reported_ghs" not in miners["bitcoincash:qa.rig2"])
    check("pool row: reported hashrate/power summed over polled miners",
          round(pool["reported_ghs"], 2) == 7348.27      # the stopped NerdQaxe adds 0
          and round(pool["power_w"], 2) == round(AXE["power"] + NERD["power"], 2))
    check("pool row: hottest chip and VR across miners (49.95 vs 28.9; 50 vs 20.3)",
          round(pool["chip_temp_c"], 2) == 49.95 and pool["vr_temp_c"] == 50)
    pool, _ = stratum.collect_sample([b], 10.0, weakref.WeakKeyDictionary(), 20.0, devices={})
    check("no polled miners -> pool reported fields are None (not 0)",
          pool["reported_ghs"] is None and pool["power_w"] is None)


def test_settings_and_api():
    cur = config.load()
    u = {"miner_api_hosts": "192.168.1.20, 192.168.1.21:8080 192.168.1.20"}
    check("settings: hosts normalized and de-duplicated",
          status.validate_updates(u, cur) is None
          and u["miner_api_hosts"] == "192.168.1.20 192.168.1.21:8080")
    for bad in ("8.8.8.8", "http://192.168.1.2", "192.168.1.2/x"):
        check(f"settings: {bad!r} refused with a message",
              "Miner addresses" in (status.validate_updates({"miner_api_hosts": bad}, cur) or ""))
    check("settings: non-string refused",
          status.validate_updates({"miner_api_hosts": ["192.168.1.2"]}, cur) is not None)
    check("settings: enabled must be a boolean",
          status.validate_updates({"miner_api_enabled": "maybe"}, cur) is not None)

    class Jobs:
        def apply_config(self, cfg):
            pass

    class Srv:
        clients = set()
    p = MinerPoller(fetch=lambda ip, port: AXE)
    st = status.StatusServer(Srv(), Jobs(), "127.0.0.1", 0, poller=p)

    async def go():
        body = json.dumps({"miner_api_hosts": "192.168.1.30", "miner_api_enabled": False})
        r = await st._route("POST", "/config", {"content-type": "application/json"},
                            body.encode())
        applied = (r[0].startswith("200") and "192.168.1.30:80" in p.targets
                   and p.enabled is False)
        body = json.dumps({"miner_api_hosts": ""})
        r2 = await st._route("POST", "/config", {"content-type": "application/json"},
                             body.encode())
        cleared = r2[0].startswith("200") and "192.168.1.30:80" not in p.targets
        r3 = await st._route("GET", "/devices", {}, b"")
        dev = json.loads(r3[2])
        return applied, cleared, dev
    applied, cleared, dev = asyncio.run(go())
    check("saving settings applies the manual list and on/off live", applied)
    check("an empty field clears the manual list", cleared)
    check("GET /devices returns the snapshot", dev.get("enabled") is False and "miners" in dev)
    st.poller = None
    r = asyncio.run(st._route("GET", "/devices", {}, b""))
    check("GET /devices without a poller -> 503", r[0].startswith("503"))
    p.close()


if __name__ == "__main__":
    test_normalize()
    test_addresses()
    test_poller()
    test_concurrency()
    test_http_client()
    test_sample_with_devices()
    test_settings_and_api()
    print(f"\n{ok} passed, {fail} failed")
    sys.exit(0 if fail == 0 else 1)
