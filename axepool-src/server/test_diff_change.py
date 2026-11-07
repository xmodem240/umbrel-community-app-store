#!/usr/bin/env python3
"""
AxePool - shares mined at the difficulty a job was sent with.

AxeOS stamps each job with the pool difficulty in force when its mining.notify
arrived (bitaxeorg/ESP-Miner stratum_v1_client.c: pool_target copied into the
job) and checks results against that job's target. After a difficulty INCREASE
the miner keeps submitting shares at the old difficulty until its next job;
AxePool only sends a new job on the same tip every 30 s, so such shares can
arrive well after the 15 s grace and were rejected as "Low difficulty share".

Drives the real submit path with real hashing (difficulty->target scaled down
so shares take ~100s of hashes). Offline:   cd server && python3 test_diff_change.py
"""
import asyncio
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import test_pipeline as tp                       # noqa: E402  (harness: Miner, FakeRPC...)
import stratum                                   # noqa: E402
from job_manager import JobManager               # noqa: E402
from stats import Stats                          # noqa: E402

ok = fail = 0


def check(label, cond):
    global ok, fail
    if cond:
        ok += 1
        print(f"PASS  {label}")
    else:
        fail += 1
        print(f"FAIL  {label}")


SCALE = 2 ** 256 // 64                           # difficulty d -> ~64*d hashes per share
stratum.target_from_difficulty = lambda d: SCALE // max(1, int(d))


async def main():
    rpc = tp.FakeRPC()
    jm = JobManager(rpc, 1, stats=Stats())
    jm.vardiff = {"enabled": False, "target_spm": 20, "min": 1, "max": 4000000}
    prev = "00000000000000000123456789abcdef00112233445566778899aabbccddeeff"
    jm._new_real(tp.make_gbt(950_000, prev, 312_512_345), clean=True)
    jm.mode = "real"
    server = stratum.StratumServer(jm)
    w = tp.FakeWriter("10.0.0.5")
    c = stratum.ClientConn(server, None, w)
    server.clients.add(c)
    await c.on_subscribe(1, ["bitaxe/test"])
    await c.on_authorize(2, [f"{tp.ADDR_A}.rig", "x"])
    m = tp.Miner(c, w)
    m.absorb()
    await c.set_difficulty(1)
    await c.send_current_job()
    m.absorb()
    job1 = list(m.notify)                         # job sent while difficulty was 1
    t1 = SCALE // 1

    # Pool raises the difficulty (vardiff/seed) without sending a new job yet.
    await c.set_difficulty(8)
    m.absorb()
    t8 = SCALE // 8
    c._diff_changed -= 25                         # 25 s later: past the 15 s grace

    params, *_ = m.find(lambda h: t8 < h <= t1, notify=job1)
    r = await m.submit(params)
    check("old-job share meeting the job's difficulty (1) is accepted 25 s after the "
          "increase to 8", r and r.get("result") is True)
    check("...and credited at the job's difficulty, not the new one",
          c._shares and c._shares[-1][1] == 1)

    params, *_ = m.find(lambda h: h > t1, notify=job1)
    r = await m.submit(params)
    check("old-job share NOT meeting even the job's difficulty is still rejected",
          r and r.get("error") and r["error"][0] == 23)

    # A job sent after the increase must meet the new difficulty.
    jm.current_job_id = jm.current_job_id      # same tip; force a fresh job id
    jm._new_real(tp.make_gbt(950_000, prev, 312_512_346), clean=False)
    await c.send_current_job()
    m.absorb()
    job2 = list(m.notify)
    check("a new job id was sent", job2[0] != job1[0])
    params, *_ = m.find(lambda h: t8 < h <= t1, notify=job2)
    r = await m.submit(params)
    check("new-job share at only the OLD difficulty is rejected",
          r and r.get("error") and r["error"][0] == 23)
    params, *_ = m.find(lambda h: h <= t8, notify=job2)
    r = await m.submit(params)
    check("new-job share at the new difficulty is accepted", r and r.get("result") is True)

    # Decrease: shares at the new, lower difficulty on the old job are fine.
    await c.set_difficulty(2)
    m.absorb()
    params, *_ = m.find(lambda h: SCALE // 2 >= h > t8, notify=job2)
    r = await m.submit(params)
    check("after a decrease, an old-job share meeting the new lower difficulty is accepted",
          r and r.get("result") is True)

    # Same job re-sent at a different difficulty (firmware may ignore the
    # duplicate notify and keep the first target): the easier one is honoured.
    await c.set_difficulty(16)
    await c.send_current_job()                   # job2 again, now at difficulty 16
    m.absorb()
    c._diff_changed -= 25
    params, *_ = m.find(lambda h: SCALE // 16 < h <= t8, notify=job2)
    r = await m.submit(params)
    check("re-sent job: a share at its first (easier) difficulty is still accepted",
          r and r.get("result") is True)

    # Vardiff must weigh late old-difficulty shares by their work, not count them
    # as full shares: 40 shares at difficulty 1 equal 2.5 shares at difficulty 16.
    c2 = stratum.ClientConn(server, None, tp.FakeWriter("10.0.0.7"))
    c2.authorized = True
    c2.difficulty, c2.target = 16, SCALE // 16
    c2.connected_mono = time.monotonic() - 300
    c2._diff_changed = time.monotonic() - 60
    c2._last_vardiff = time.monotonic() - 100
    now = time.monotonic()
    c2._shares.extend((now - 59 + i, 1) for i in range(40))      # stale low-diff shares
    jm.vardiff = {"enabled": True, "target_spm": 20, "min": 1, "max": 4000000}
    await c2.maybe_retarget()
    check(f"vardiff weighs shares by work: 40 diff-1 shares/min at diff 16 lowers, "
          f"not raises (16 -> {c2.difficulty})", c2.difficulty < 16)

    if server.submit_tasks:
        await asyncio.gather(*list(server.submit_tasks))
    print(f"\n{ok} passed, {fail} failed")
    return fail == 0


if __name__ == "__main__":
    sys.exit(0 if asyncio.run(main()) else 1)
