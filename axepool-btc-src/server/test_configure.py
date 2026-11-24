#!/usr/bin/env python3
"""
AxePool - version-rolling mask negotiation (BIP310 mining.configure).

The pool grants the bits of its own mask (1fffe000) that the miner also asked
for, and checks shares against that granted mask. Bitaxe / NerdQaxe ask for
ffffffff, so their reply must stay exactly what it was before this change.

Drives the real ClientConn.on_configure / on_submit with real hashing (the
share target is scaled down, as in test_rejects.py).
Offline:   cd server && python3 test_configure.py
"""
import asyncio
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import test_pipeline as tp                       # noqa: E402  (sets APP_DATA_DIR, harness)
import stratum                                   # noqa: E402
from stratum import negotiate_version_mask       # noqa: E402
from stats import Stats                          # noqa: E402
from job_manager import JobManager               # noqa: E402

ok = fail = 0


def check(label, cond):
    global ok, fail
    if cond:
        ok += 1
        print(f"PASS  {label}")
    else:
        fail += 1
        print(f"FAIL  {label}")


SCALE = 2 ** 256 // 64
stratum.target_from_difficulty = lambda d: SCALE // max(1, int(d))
# What ESP-Miner (AxeOS) sends, byte for byte (components/stratum/stratum_api.c).
ESP_MINER = [["version-rolling"], {"version-rolling.mask": "ffffffff"}]


def test_negotiate():
    pool = 0x1fffe000
    check("miner asks for every bit -> the pool's full mask",
          negotiate_version_mask({"version-rolling.mask": "ffffffff"}) == pool)
    check("miner asks for exactly the pool's mask -> unchanged",
          negotiate_version_mask({"version-rolling.mask": "1fffe000"}) == pool)
    check("narrower request 00fff000 -> only the common bits 00ffe000",
          negotiate_version_mask({"version-rolling.mask": "00fff000"}) == 0x00ffe000)
    check("upper-case hex accepted",
          negotiate_version_mask({"version-rolling.mask": "00FFF000"}) == 0x00ffe000)
    check("short hex is a number, not a prefix: 'e000' -> 0000e000",
          negotiate_version_mask({"version-rolling.mask": "e000"}) == 0x0000e000)
    check("no bit in common -> 0",
          negotiate_version_mask({"version-rolling.mask": "00001fff"}) == 0)
    for label, ext in (("no second parameter", None), ("empty object", {}),
                       ("not an object", ["ffffffff"]),
                       ("mask not a string", {"version-rolling.mask": 0xffffffff}),
                       ("mask not hex", {"version-rolling.mask": "zzzzzzzz"}),
                       ("mask longer than 32 bits", {"version-rolling.mask": "1ffffffff"}),
                       ("mask empty", {"version-rolling.mask": ""})):
        check(f"{label} -> the pool's mask, as before",
              negotiate_version_mask(ext) == pool)


async def connect(server, ip, configure):
    w = tp.FakeWriter(ip)
    c = stratum.ClientConn(server, None, w)
    server.clients.add(c)
    reply = None
    if configure is not None:
        await c.on_configure(1, configure)
        reply = w.messages()[0]
    await c.on_subscribe(2, ["bitaxe/test"])
    await c.on_authorize(3, [f"{tp.ADDR_A}.r{ip.rsplit('.', 1)[1]}", "x"])
    m = tp.Miner(c, w)
    await c.set_difficulty(4)
    await c.send_current_job()
    m.absorb()
    return c, m, reply


def find_split(m, rolled_ver, base_ver, t):
    """A share that meets `t` when hashed with `rolled_ver` but NOT with
    `base_ver`, so accepting it proves the server used the rolled bits and
    rejecting it proves the server dropped them (never a lucky hash)."""
    while True:
        params, hdr, _cb, h = m.find(lambda x: x <= t, version=f"{rolled_ver:08x}")
        other, _ = m.header(params[2], params[3], params[4], version=f"{base_ver:08x}")
        if int.from_bytes(tp.sha256d(other), "little") > t:
            return params


async def test_end_to_end():
    jm = JobManager(tp.FakeRPC(), 1, stats=Stats())
    jm.vardiff = {"enabled": False, "target_spm": 20, "min": 1, "max": 4000000}
    prev = "00000000000000000fedcba98765432100112233445566778899aabbccddeeff"
    jm._new_real(tp.make_gbt(950_100, prev, 312_512_345), clean=True)
    jm.mode = "real"
    server = stratum.StratumServer(jm)
    t4 = SCALE // 4
    base = 0x20000000

    c, m, reply = await connect(server, "10.0.0.21", ESP_MINER)
    check("Bitaxe request: reply exactly as before this change",
          reply == {"id": 1, "result": {"version-rolling": True,
                                        "version-rolling.mask": "1fffe000"},
                    "error": None})
    p = find_split(m, base ^ 0x10000000, base, t4)
    r = await m.submit(p + ["10000000"])
    check("Bitaxe: share rolling bit 28 (inside 1fffe000) accepted",
          r.get("result") is True)

    c, m, reply = await connect(server, "10.0.0.22",
                                [["version-rolling"], {"version-rolling.mask": "00fff000"}])
    check("narrow request: granted 00ffe000 and the reply says so",
          c.version_mask == "00ffe000"
          and reply["result"] == {"version-rolling": True,
                                  "version-rolling.mask": "00ffe000"})
    p = find_split(m, base ^ 0x00004000, base, t4)
    r = await m.submit(p + ["00004000"])
    check("narrow grant: share rolling a granted bit (14) accepted",
          r.get("result") is True)
    before = c.reject_kinds["low_diff"]
    p = find_split(m, base ^ 0x10000000, base, t4)
    r = await m.submit(p + ["10000000"])
    check("narrow grant: share rolling bit 28 (not granted) rejected as low difficulty",
          r["error"] and r["error"][0] == 23 and c.reject_kinds["low_diff"] == before + 1)

    c, m, reply = await connect(server, "10.0.0.23",
                                [["version-rolling"], {"version-rolling.mask": "00001fff"}])
    check("no common bit: version rolling refused, nothing granted",
          c.version_mask is None and reply["result"] == {"version-rolling": False})
    p = find_split(m, base ^ 0x00004000, base, t4)
    r = await m.submit(p + ["00004000"])
    check("rolling refused: a share that rolls anyway is rejected",
          r["error"] and r["error"][0] == 23)
    p = find_split(m, base, base ^ 0x00004000, t4)
    r = await m.submit(p)
    check("rolling refused: a plain share (job version) is still accepted",
          r.get("result") is True)

    c, m, reply = await connect(server, "10.0.0.24", [["minimum-difficulty"], {}])
    check("configure without version-rolling: empty result, no mask",
          reply["result"] == {} and c.version_mask is None)


if __name__ == "__main__":
    test_negotiate()
    asyncio.run(test_end_to_end())
    print(f"\n{ok} passed, {fail} failed")
    sys.exit(0 if fail == 0 else 1)
