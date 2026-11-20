#!/usr/bin/env python3
"""
AxePool - vardiff floor at the miner's own ASIC difficulty.

Newer AxeOS reports `asicDifficulty`, the difficulty the ASIC filters nonces
at, and warns when the pool difficulty is below it. With vardiff on, AxePool
never sets a linked miner below that value; without the field (released AxeOS
up to v2.15.4, NerdQaxe) nothing changes. The Settings maximum always wins.

Drives the real ClientConn methods with a fake transport, as in
test_vardiff_seed.py. Offline:   cd server && python3 test_asic_floor.py
"""
import asyncio
import json
import os
import sys
import tempfile
import time

os.environ["APP_DATA_DIR"] = tempfile.mkdtemp(prefix="axepool-asic-floor-test-")
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import logging                                      # noqa: E402
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
with open(os.path.join(HERE, "fixtures",
                       "nerdqaxe_v1.1.0.1_nerdqaxepp_shutdown.json")) as f:
    NERD = json.load(f)


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
    c = ClientConn(Server(jobs or Jobs()), None, Writer())
    c.authorized = True
    c.raw_user = "bitcoincash:qa.naja"
    c.difficulty = diff
    c.connected_mono = time.monotonic() - age
    return c


def silent(c):
    """A miner that sent no share for two minutes, last retarget long ago."""
    t = time.monotonic()
    c.connected_mono = t - 600
    c._diff_changed = t - 600
    c._last_vardiff = t - 600
    return c


def run(coro):
    return asyncio.run(coro)


def test_normalize():
    check("released AxeOS v2.15.4 has no asicDifficulty -> None",
          "asicDifficulty" not in AXE and normalize(AXE)["asic_difficulty"] is None)
    check("NerdQaxe has no asicDifficulty -> None",
          normalize(NERD)["asic_difficulty"] is None)
    check("newer AxeOS asicDifficulty 256 is read",
          normalize(dict(AXE, asicDifficulty=256))["asic_difficulty"] == 256)
    for bad in ("256", "abc", 0, -512, 2 ** 33, True, None, float("nan")):
        got = normalize(dict(AXE, asicDifficulty=bad))["asic_difficulty"]
        ok_ = got is None or (bad == "256" and got == 256)
        check(f"asicDifficulty {bad!r}: ignored unless a sane number ({got!r})", ok_)


def test_bounds():
    c = conn()
    check("no ASIC difficulty known: bounds are the Settings min/max",
          c._diff_bounds() == (128, 4_000_000))
    c.note_device({"asic_difficulty": 256.0})
    check("ASIC difficulty 256 above the Settings min 128 -> floor 256",
          c._diff_bounds() == (256, 4_000_000))
    c.note_device({"asic_difficulty": 64.0})
    check("ASIC difficulty 64 below the Settings min -> Settings min 128 stays",
          c._diff_bounds() == (128, 4_000_000))
    c = conn(Jobs(hi=4000))
    c.note_device({"asic_difficulty": 8192.0})
    check("ASIC difficulty above the Settings max -> the max wins (4000)",
          c._diff_bounds() == (4000, 4000))
    c.note_device({"asic_difficulty": 256.0})
    c.note_device(None)
    check("miner no longer linked -> floor forgotten",
          c.asic_floor is None and c._diff_bounds() == (128, 4000))
    c.note_device({"asic_difficulty": None})
    check("linked miner without the field -> no floor", c.asic_floor is None)


def test_vardiff():
    c = silent(conn(diff=1000))
    run(c.maybe_retarget())
    check("(control) silent miner without a floor eases 1000 -> 250",
          c.difficulty == 250)

    c = silent(conn(diff=1000))
    c.note_device({"asic_difficulty": 256.0})
    run(c.maybe_retarget())
    check("silent miner with ASIC difficulty 256 eases 1000 -> 256, not 250",
          c.difficulty == 256)

    c = silent(conn(diff=1000))
    c.note_device({"asic_difficulty": 512.0})
    t = time.monotonic()
    for i in range(6):                        # 3 shares/min for 2 min at diff 1000
        c._shares.append((t - 118 + i * 20, 1000))
    run(c.maybe_retarget())
    check("slow miner: target-rate drop (1000 -> 150) stops at the floor 512",
          c.difficulty == 512)

    c = silent(conn(diff=200))
    c.note_device({"asic_difficulty": 256.0})
    t = time.monotonic()
    for i in range(40):                       # 20/min at diff 200: on target
        c._shares.append((t - 118 + i * 3, 200))
    run(c.maybe_retarget())
    check("already below a newly reported floor: next retarget lifts 200 -> 256",
          c.difficulty == 256)

    c = silent(conn(Jobs(enabled=False), diff=100))
    c.note_device({"asic_difficulty": 256.0})
    run(c.maybe_retarget())
    check("vardiff off: the fixed difficulty is left alone (100)",
          c.difficulty == 100)


def test_seed_and_suggest():
    dev = normalize(dict(AXE, asicDifficulty=16384))
    c = conn()
    c.note_device(dev)
    run(c.maybe_seed(dev))
    raw = seed_difficulty(dev["hashrate_10m_ghs"], 20)
    check(f"seed {raw:.0f} below ASIC difficulty 16384 -> seeded at 16384",
          raw < 16384 and c.difficulty == 16384)

    dev = normalize(dict(AXE, asicDifficulty=256))
    c = conn()
    c.note_device(dev)
    run(c.maybe_seed(dev))
    check("seed above the floor is unchanged by it",
          c.difficulty == round(seed_difficulty(dev["hashrate_10m_ghs"], 20)))

    c = conn()
    c.authorized = False
    c.note_device({"asic_difficulty": 256.0})
    run(c.on_suggest_difficulty(7, [100]))
    check("miner suggests 100 with ASIC difficulty 256 -> starts at 256",
          c.suggested_diff == 256)
    c = conn()
    c.authorized = False
    run(c.on_suggest_difficulty(7, [100]))
    check("(control) same suggestion with no floor -> 128 (Settings min)",
          c.suggested_diff == 128)


if __name__ == "__main__":
    test_normalize()
    test_bounds()
    test_vardiff()
    test_seed_and_suggest()
    print(f"\n{ok} passed, {fail} failed")
    sys.exit(0 if fail == 0 else 1)
