#!/usr/bin/env python3
"""
AxePool - difficulty seeding from the miner's API, and the vardiff window fix.

Drives the real ClientConn.maybe_seed / maybe_retarget with a fake transport,
using the miner record normalized from the maintainer's real AxeOS response.
Time is moved by rewriting the connection's monotonic timestamps.

Offline, standard library only:   cd server && python3 test_vardiff_seed.py
"""
import asyncio
import json
import os
import sys
import tempfile
import time

os.environ["APP_DATA_DIR"] = tempfile.mkdtemp(prefix="axepool-seed-test-")
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import logging                                      # noqa: E402
import stratum                                      # noqa: E402
from stratum import ClientConn, seed_difficulty    # noqa: E402
from miner_poller import normalize                  # noqa: E402

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


with open(os.path.join(HERE, "fixtures", "axeos_v2.15.4_naja_duo.json")) as f:
    AXE = json.load(f)
DEVICE = normalize(AXE)                 # 7348 GH/s now, 7132 GH/s 10-minute average


class Writer:
    def __init__(self):
        self.sent = []

    def get_extra_info(self, k):
        return ("192.168.50.74", 40000) if k == "peername" else None

    def write(self, b):
        self.sent.append(json.loads(b))

    async def drain(self):
        pass


class Jobs:
    def __init__(self, enabled=True, target=20, lo=128, hi=4_000_000):
        self.vardiff = {"enabled": enabled, "target_spm": target, "min": lo, "max": hi}
        self.share_difficulty = 1000


class Server:
    def __init__(self, jobs):
        self.jobs = jobs
        self.clients = set()


def conn(jobs=None, diff=1000, age=5.0):
    j = jobs or Jobs()
    c = ClientConn(Server(j), None, Writer())
    c.authorized = True
    c.raw_user = "bitcoincash:qa.naja"
    c.difficulty = diff
    c.connected_mono = time.monotonic() - age
    return c


def run(coro):
    return asyncio.run(coro)


def diffs_sent(c):
    return [m["params"][0] for m in c.writer.sent if m.get("method") == "mining.set_difficulty"]


def test_formula():
    d = seed_difficulty(7131.84, 20)
    # 7131.84e9 H/s at difficulty d -> shares/min = H*60/(d*2^32) must equal 20.
    check(f"formula: {d:.0f} gives exactly 20 shares/min at 7131.84 GH/s",
          abs(7131.84e9 * 60 / (d * 2 ** 32) - 20) < 1e-9)
    check("formula: doubling hashrate doubles difficulty",
          abs(seed_difficulty(2000, 20) / seed_difficulty(1000, 20) - 2) < 1e-12)
    check("formula: doubling target share rate halves difficulty",
          abs(seed_difficulty(1000, 10) / seed_difficulty(1000, 20) - 2) < 1e-12)


def test_seed():
    c = conn()
    new = run(c.maybe_seed(DEVICE))
    expect = round(seed_difficulty(DEVICE["hashrate_10m_ghs"], 20))
    check(f"seeds from the 10-minute hashrate: 1000 -> {expect}",
          new == expect and c.difficulty == expect and diffs_sent(c) == [expect])
    check("seed marks the connection and holds off the next retarget",
          c.seeded and time.monotonic() - c._last_vardiff < 1)
    faster = dict(DEVICE, hashrate_10m_ghs=DEVICE["hashrate_10m_ghs"] * 3)
    check("seed is one-shot: a later, very different reading does not reseed",
          run(c.maybe_seed(faster)) is None and diffs_sent(c) == [expect])

    c = conn()
    dev = dict(DEVICE, hashrate_10m_ghs=None)
    run(c.maybe_seed(dev))
    check("falls back to the 1-minute hashrate when the 10-minute one is missing",
          c.difficulty == round(seed_difficulty(DEVICE["hashrate_1m_ghs"], 20)))

    c = conn(Jobs(enabled=False))
    check("vardiff off: fixed difficulty is authoritative, no seed",
          run(c.maybe_seed(DEVICE)) is None and c.difficulty == 1000 and not c.seeded)

    c = conn()
    c.internal = True
    check("internal diagnostics client never seeded",
          run(c.maybe_seed(DEVICE)) is None and c.difficulty == 1000)

    c = conn()
    stopped = dict(DEVICE, state="stopped", hashrate_ghs=0, hashrate_1m_ghs=0,
                   hashrate_10m_ghs=0)
    check("stopped miner: no seed yet, and can still seed once it is mining",
          run(c.maybe_seed(stopped)) is None and not c.seeded
          and run(c.maybe_seed(DEVICE)) is not None)

    for state in ("paused", "overheat", "idle"):
        c = conn()
        # Firmware can keep reporting its last hashrate while not hashing.
        check(f"{state} miner with a stale non-zero hashrate is not seeded",
              run(c.maybe_seed(dict(DEVICE, state=state))) is None and c.difficulty == 1000)

    c = conn(age=stratum.SEED_WINDOW + 5)
    check("late (connection older than the seed window): no seed, marked done",
          run(c.maybe_seed(DEVICE)) is None and c.difficulty == 1000 and c.seeded)

    near = round(seed_difficulty(DEVICE["hashrate_10m_ghs"], 20) * 1.1)
    c = conn(diff=near)
    check("within 25% of the right value: left alone (no churn), marked done",
          run(c.maybe_seed(DEVICE)) is None and c.difficulty == near and c.seeded
          and diffs_sent(c) == [])

    c = conn(Jobs(hi=2000))
    run(c.maybe_seed(DEVICE))
    check("clamped to the vardiff maximum", c.difficulty == 2000)
    c = conn(Jobs(lo=50000))
    run(c.maybe_seed(DEVICE))
    check("clamped to the vardiff minimum", c.difficulty == 50000)

    c = conn()
    check("no device record: nothing happens", run(c.maybe_seed(None)) is None
          and not c.seeded)


def test_window_fix():
    """After a seed, the next retarget must not count the fast pre-seed shares."""
    c = conn(age=150)
    now = time.monotonic()
    pre = 1000
    # 85 s at difficulty 1000 for a ~7.1 TH/s miner (~100 shares/min), all of
    # it BEFORE the seed 60 s ago: from 146 s ago to 62 s ago.
    for i in range(140):
        c._shares.append((now - 146 + i * 0.6, pre))
    assert max(t for t, _ in c._shares) < now - 60
    run(c.maybe_seed(DEVICE))
    seeded = c.difficulty
    # Pretend the seed happened 60 s ago and since then shares arrived at the
    # target rate (20/min) at the new difficulty.
    c._diff_changed = now - 60
    c._last_vardiff = now - 60
    for i in range(20):
        c._shares.append((now - 59 + i * 3, seeded))
    run(c.maybe_retarget())
    check(f"retarget after a seed sees ~20/min and keeps {seeded} (no overshoot)",
          c.difficulty == seeded)

    # The same situation measured the old way (window back to the connect time)
    # would have read ~100 shares/min and multiplied the difficulty.
    window = min(stratum.VARDIFF_WINDOW, 150)
    old_spm = c.shares_in(window) / (window / 60)
    check(f"(control) the old window would have read {old_spm:.0f}/min and overshot",
          old_spm > 20 * 1.25)

    # Right after a change there is too little data: no retarget at all.
    c2 = conn(age=600)
    c2._diff_changed = time.monotonic() - 10
    c2._last_vardiff = time.monotonic() - 100
    before = c2.difficulty
    run(c2.maybe_retarget())
    check("no retarget until 30 s of shares at the current difficulty exist",
          c2.difficulty == before)

    # A miner with no change history still retargets normally (upstream behaviour).
    c3 = conn(age=200)
    t = time.monotonic()
    for i in range(240):                       # 120 shares/min for 120 s at diff 1000
        c3._shares.append((t - 119.5 + i * 0.5, 1000))
    run(c3.maybe_retarget())
    check("normal retarget unchanged without a seed: 120/min -> about 6x difficulty",
          5000 <= c3.difficulty <= 7000)


if __name__ == "__main__":
    test_formula()
    test_seed()
    test_window_fix()
    print(f"\n{ok} passed, {fail} failed")
    sys.exit(0 if fail == 0 else 1)
