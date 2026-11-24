#!/usr/bin/env python3
"""
AxePool - reject reasons: every rejected share is counted by why it was
rejected (low difficulty / stale / duplicate / other), per connection and in
the persisted lifetime stats.

Drives the real submit path with real hashing (difficulty->target scaled down).
Offline:   cd server && python3 test_rejects.py
"""
import asyncio
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import test_pipeline as tp                       # noqa: E402  (sets APP_DATA_DIR, harness)
import stratum                                   # noqa: E402
import status                                    # noqa: E402
import stats as stats_mod                        # noqa: E402
from stats import Stats, REJECT_KINDS            # noqa: E402
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


async def main():
    path = stats_mod.stats_path()
    if path and os.path.exists(path):
        os.remove(path)
    st = Stats()
    jm = JobManager(tp.FakeRPC(), 1, stats=st)
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
    await c.set_difficulty(4)
    await c.send_current_job()
    m.absorb()
    job = list(m.notify)
    t4 = SCALE // 4

    def kinds():
        return dict(c.reject_kinds)

    check("starts at zero for every reason",
          kinds() == dict.fromkeys(REJECT_KINDS, 0) and set(REJECT_KINDS) ==
          {"low_diff", "stale", "duplicate", "other"})

    params, *_ = m.find(lambda h: h > t4, notify=job)
    r = await m.submit(params)
    check("share below the job's difficulty -> low_diff (error 23)",
          r["error"][0] == 23 and kinds()["low_diff"] == 1)

    good, *_ = m.find(lambda h: h <= t4, notify=job)
    r1 = await m.submit(good)
    r2 = await m.submit(good)
    check("same share twice -> accepted once, then duplicate (error 22)",
          r1.get("result") is True and r2["error"][0] == 22 and kinds()["duplicate"] == 1)

    bogus = list(good)
    bogus[1] = "deadbeef-no-such-job"
    r = await m.submit(bogus)
    check("unknown job id -> stale (error 21)", r["error"][0] == 21 and kinds()["stale"] == 1)

    r = await m.submit(good[:3])
    bad = list(good)
    bad[4] = "zzzzzzzz"
    r2 = await m.submit(bad)
    check("malformed submits -> other (error 20), twice",
          r["error"][0] == 20 and r2["error"][0] == 20 and kinds()["other"] == 2)

    # Chain tip replaced: work on the old job is stale even though it is known.
    held, *_ = m.find(lambda h: SCALE // 64 < h <= t4, notify=job)
    jm._new_real(tp.make_gbt(950_001, "11" * 32, 312_500_000), clean=True)
    r = await m.submit(held)
    check("share on a replaced chain tip -> stale", r["error"][0] == 21 and kinds()["stale"] == 2)

    check("per-reason counts add up to the connection's total",
          sum(kinds().values()) == c.rejected == 6)
    check("lifetime stats carry the same breakdown",
          st.lifetime_rejected_by == kinds() and st.lifetime_rejected == 6)

    import time as _t
    row = c.stats_snapshot(_t.time())
    check("/status miner row has the breakdown", row["rejects"] == kinds() and row["rejected"] == 6)
    snap = status.build_snapshot(server, jm)
    check("/status pool block has the lifetime breakdown",
          snap["pool"]["rejected_by"] == kinds())
    prom = status.build_metrics(server, jm)
    check("Prometheus exports rejects by reason",
          'solobch_shares_rejected_by_reason_total{reason="low_diff"} 1' in prom
          and 'solobch_shares_rejected_by_reason_total{reason="stale"} 2' in prom)

    # Unauthorized connection: counted as other, never crashes.
    c2 = stratum.ClientConn(server, None, tp.FakeWriter("10.0.0.6"))
    await c2.on_submit(9, good)
    check("unauthorized submit -> other", c2.reject_kinds["other"] == 1 and c2.rejected == 1)

    # Persistence: survives a restart; an unknown kind is filed under other.
    st.record_reject("something-new")
    st._save(force=True)
    again = Stats()
    check("breakdown persists across a restart (unknown kind filed as other)",
          again.lifetime_rejected_by == dict(kinds(), other=kinds()["other"] + 2)
          and again.lifetime_rejected == 8)

    # Upgrade from a stats file written before the breakdown existed.
    with open(path) as f:
        old = json.load(f)
    del old["lifetime_rejected_by"]
    with open(path, "w") as f:
        json.dump(old, f)
    up = Stats()
    check("pre-0.5.0 stats file loads: total kept, breakdown starts at zero",
          up.lifetime_rejected == 8 and up.lifetime_rejected_by == dict.fromkeys(REJECT_KINDS, 0))
    for junk in ("oops", [1, 2], {"low_diff": "many", "stale": -5, "x": 3}):
        old["lifetime_rejected_by"] = junk
        with open(path, "w") as f:
            json.dump(old, f)
        g = Stats()
        check(f"corrupt breakdown {junk!r} handled",
              g.lifetime_rejected == 8 and set(g.lifetime_rejected_by) == set(REJECT_KINDS)
              and all(isinstance(v, int) and v >= 0 for v in g.lifetime_rejected_by.values()))
    up.reset_lifetime() if hasattr(up, "reset_lifetime") else None

    if server.submit_tasks:
        await asyncio.gather(*list(server.submit_tasks))
    print(f"\n{ok} passed, {fail} failed")
    return fail == 0


if __name__ == "__main__":
    sys.exit(0 if asyncio.run(main()) else 1)
