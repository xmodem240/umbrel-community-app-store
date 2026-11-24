#!/usr/bin/env python3
"""
AxePool - metrics store + sampler tests.

Covers the 10s/1m/1h store (buffering, batched writes, exact rollups, late
samples, retention, the read API) and the sampler that feeds it (no double
counting, reject deltas, reconnects). Uses a fake clock, so it runs in seconds.

Offline, standard library only:   cd server && python3 test_metrics_store.py
"""
import asyncio
import json
import os
import sys
import tempfile
import time
import weakref

DATA_DIR = tempfile.mkdtemp(prefix="axepool-metrics-test-")
os.environ["APP_DATA_DIR"] = DATA_DIR
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import logging                                  # noqa: E402
import metrics_store as ms                      # noqa: E402
from metrics_store import MetricsStore, bucket_hashrate   # noqa: E402
import stratum                                  # noqa: E402
from status import StatusServer                 # noqa: E402

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


class Clock:
    def __init__(self, t):
        self.t = float(t)

    def __call__(self):
        return self.t


T0 = 1_780_000_000 // 3600 * 3600              # an hour boundary, a real 2026 epoch


def sample(work=0.0, hr=0.0, acc=0, rej=0, diff=None, workers=1):
    return {"work": work, "hr_5m": hr, "accepted": acc, "rejected": rej,
            "difficulty": diff, "workers": workers}


def store(clock, name):
    return MetricsStore(os.path.join(DATA_DIR, name), now=clock)


def test_store():
    # ---- disabled store never fails ----------------------------------------
    off = MetricsStore("", now=Clock(T0))
    off.add_sample(T0, sample(), {})
    check("store with no data dir is disabled and harmless",
          not off.enabled and off.flush() == 0 and off.series("1h")["points"] == [])
    off.close()

    # ---- buffered samples are visible before they are written --------------
    c = Clock(T0 + 5)
    s = store(c, "a.sqlite")
    s.add_sample(T0 + 3, sample(work=100, hr=7e12, acc=4, workers=2),
                 {"addr.rig1": sample(work=60, acc=3, diff=4096),
                  "addr.rig2": sample(work=40, acc=1, diff=2048)})
    pts = s.series("1h")["points"]
    check("sample timestamp snapped to its 10 s bucket", pts and pts[0][0] == T0)
    check("buffered (unwritten) sample appears in the 1h series at once",
          len(pts) == 1 and pts[0][3] == 4)
    check("10 s hashrate is exact from work", pts[0][1] == round(bucket_hashrate(100, 10), 2))
    check("miner series is separate from the pool series",
          s.series("1h", miner="addr.rig1")["points"][0][3] == 3)

    # ---- batching: nothing is written until the flush interval -------------
    check("no flush due before FLUSH_INTERVAL", not s.due())
    c.t += ms.FLUSH_INTERVAL
    check("flush due after FLUSH_INTERVAL", s.due())

    # Regression: the flush is stamped a few ms after its 10 s tick, so the
    # check 6 ticks later is a hair under 60 s. It must still be due, or every
    # batch slips to 70 s (seen in a live run).
    jc = Clock(T0 + 0.05)
    js = store(jc, "jitter.sqlite")
    jc.t = T0 + 0.06
    js.flush()                                   # stamped at tick + 0.06
    jc.t = T0 + 50.05
    early = js.due()
    jc.t = T0 + 60.05                            # 59.99 s after the stamp
    check("flush not due one tick early, but due on the 6th tick despite jitter",
          not early and js.due())
    js.close()

    # Fill the rest of minute T0 (6 samples total), flush mid-way into minute 2.
    for i in range(1, 6):
        s.add_sample(T0 + i * 10, sample(work=100, hr=7e12, acc=4, workers=2 + (i == 3)),
                     {"addr.rig1": sample(work=60, acc=3, diff=4096 + i),
                      "addr.rig2": sample(work=40, acc=1, diff=2048)})
    c.t = T0 + 65                                # minute T0 finished, T0+60 is open
    s.add_sample(T0 + 60, sample(work=999, acc=9), {})
    n = s.flush()
    check("one batched write stored every buffered row", n == 6 * 3 + 1)

    m1 = s.series("24h")
    check("24h range reads the 1-minute table", m1["table"] == "m1" and m1["bucket"] == 60)
    check("completed minute rolled up; open minute not rolled up",
          [p[0] for p in m1["points"]] == [T0])
    p = m1["points"][0]
    check("1m hashrate exact: summed work over 60 s",
          p[1] == round(bucket_hashrate(600, 60), 2))
    check("1m accepted is the sum", p[3] == 24)
    check("1m workers is the max seen in the minute", p[6] == 3)
    rig1 = s.series("24h", miner="addr.rig1")["points"][0]
    check("1m difficulty is the last value in the minute", rig1[5] == 4096 + 5)

    # ---- late sample for an already rolled-up minute is recomputed ---------
    c.t = T0 + 125
    s.add_sample(T0 + 70, sample(work=1, acc=1), {})          # minute T0+60
    s.flush()
    m1 = s.series("24h")["points"]
    check("minute T0+60 rolled up once it closed",
          [q[0] for q in m1] == [T0, T0 + 60] and m1[1][3] == 10)
    s.add_sample(T0 + 80, sample(work=5, acc=2), {})          # late, same minute
    s.flush()
    m1 = s.series("24h")["points"]
    check("late sample re-rolls its minute (INSERT OR REPLACE, no duplicate row)",
          len(m1) == 2 and m1[1][3] == 12)

    # ---- since: only newer points -----------------------------------------
    allp = s.series("1h")["points"]
    newer = s.series("1h", since=T0 + 30)["points"]
    check("since returns only points after it",
          newer and all(q[0] > T0 + 30 for q in newer)
          and len(newer) == len([q for q in allp if q[0] > T0 + 30]))

    # ---- bad input ---------------------------------------------------------
    try:
        s.series("2h")
        check("unknown range rejected", False)
    except ValueError:
        check("unknown range rejected", True)

    # ---- persistence across restart ----------------------------------------
    s.close()
    s2 = store(c, "a.sqlite")
    check("rows survive a close/reopen", len(s2.series("1h")["points"]) == len(allp))
    check("miner list includes both rigs",
          set(s2.miners()) == {"addr.rig1", "addr.rig2"})
    s2.close()


def test_hours_and_retention():
    c = Clock(T0)
    s = store(c, "b.sqlite")
    # Two full hours of one sample per minute (enough for the rollup path).
    for minute in range(120):
        ts = T0 + minute * 60
        c.t = ts + 1
        s.add_sample(ts, sample(work=10, hr=1e12, acc=1), {"m": sample(work=10, acc=1)})
        if minute % 10 == 9:
            s.flush()
    c.t = T0 + 2 * 3600 + 61
    s.flush()
    h = s.series("7d")
    check("7d range reads the 1-hour table", h["table"] == "h1")
    check("both completed hours rolled up, exact work",
          [q[0] for q in h["points"]] == [T0, T0 + 3600]
          and h["points"][0][1] == round(bucket_hashrate(600, 3600), 2)
          and h["points"][0][3] == 60)

    # Jump 25 hours: s10 retention (24 h) drops the old 10 s rows on the next prune.
    s._last_prune = 0
    c.t = T0 + 27 * 3600
    s.add_sample(c.t, sample(work=1, acc=1), {})
    s.flush()
    s10_old = s._pool.submit(lambda: s._db.execute(
        "SELECT COUNT(*) FROM s10 WHERE ts < ?", (int(c.t - 24 * 3600),)).fetchone()[0]).result()
    m1_kept = s._pool.submit(lambda: s._db.execute(
        "SELECT COUNT(*) FROM m1 WHERE ts < ?", (T0 + 7200,)).fetchone()[0]).result()
    check("10 s rows older than 24 h pruned", s10_old == 0)
    check("1 m rows inside 30 days kept", m1_kept == 240)

    s._last_prune = 0
    c.t = T0 + 31 * 86400
    s.flush()
    m1_left = s._pool.submit(lambda: s._db.execute(
        "SELECT COUNT(*) FROM m1 WHERE ts < ?", (T0 + 7200,)).fetchone()[0]).result()
    h1_left = s._pool.submit(lambda: s._db.execute(
        "SELECT COUNT(*) FROM h1").fetchone()[0]).result()
    check("1 m rows older than 30 days pruned", m1_left == 0)
    check("1 h rows inside a year kept", h1_left >= 4)
    s.close()


def test_write_failure():
    c = Clock(T0)
    s = store(c, "c.sqlite")
    s.add_sample(T0, sample(work=1, acc=1), {})
    real = s._write

    def broken(rows, now):
        raise OSError("disk full (simulated)")
    s._write = broken
    check("failed write returns 0 and does not raise", s.flush() == 0)
    check("rows kept in the buffer after a failed write", len(s._buf) == 1)
    s._write = real
    check("rows written on the next successful flush", s.flush() == 1)
    # Bound: a long outage cannot grow memory without limit.
    s._write = broken
    for i in range(80000):
        s._buf.append((T0 + i, "", 0.0, 0.0, 0, 0, None, 0, None, None, None, None))
    s.flush()
    check("buffer bounded during a long write outage",
          len(s._buf) <= int(3600 / ms.SAMPLE_INTERVAL) * 64)
    s._write = real
    s.close()


class FakeConn:
    def __init__(self, user, internal=False, authorized=True, diff=1024.0):
        self.raw_user = user
        self.internal = internal
        self.authorized = authorized
        self.difficulty = diff
        self.rejected = 0
        self._shares = []

    def window_hashrate(self, window_s):
        return 1e12


def test_sampler():
    a = FakeConn("addr.rig1")
    b = FakeConn("addr.rig1")                      # duplicate identity (reconnect ghost)
    d = FakeConn("diag", internal=True)
    u = FakeConn(None, authorized=False)
    for conn in (a, b, d, u):
        conn._shares = [(1.0, 100.0), (5.0, 100.0), (11.0, 200.0)]
    last = weakref.WeakKeyDictionary()
    a.rejected = 2
    pool, miners = stratum.collect_sample([a, b, d, u], 0.0, last, now_mono=10.0)
    check("internal and unauthorized connections excluded",
          set(miners) == {"addr.rig1"})
    check("duplicate identity summed into one row",
          miners["addr.rig1"]["accepted"] == 4 and miners["addr.rig1"]["work"] == 400)
    check("only shares inside (since, now] counted (share at t=11 excluded)",
          pool["work"] == 400)
    check("first sample counts the rejects so far", pool["rejected"] == 2)
    pool, miners = stratum.collect_sample([a, b], 10.0, last, now_mono=20.0)
    check("next interval counts only the newer share, no double counting",
          pool["work"] == 400 and pool["accepted"] == 2)
    check("rejects are a delta: none new -> 0", pool["rejected"] == 0)
    a.rejected = 5
    pool, _ = stratum.collect_sample([a, b], 20.0, last, now_mono=30.0)
    check("reject delta counts only the new rejects", pool["rejected"] == 3)
    fresh = FakeConn("addr.rig1")                  # reconnect: counter restarts at 0
    fresh.rejected = 1
    pool, _ = stratum.collect_sample([fresh], 30.0, last, now_mono=40.0)
    check("a reconnected connection's counter starts fresh (never negative)",
          pool["rejected"] == 1)
    pool, miners = stratum.collect_sample([], 40.0, last, now_mono=50.0)
    check("no miners -> zero pool row with workers 0",
          miners == {} and pool["workers"] == 0 and pool["work"] == 0)


def test_http():
    c = Clock(T0 + 5)
    s = store(c, "d.sqlite")
    s.add_sample(T0, sample(work=1, acc=1), {"addr.rig1": sample(work=1, acc=1)})
    srv = StatusServer(None, None, "127.0.0.1", 0, metrics=s)

    async def get(path):
        st, ct, body = await srv._route("GET", path, {}, b"")
        return st, json.loads(body)

    async def run():
        st, out = await get("/series?range=1h")
        check("GET /series 200 with points", st.startswith("200") and len(out["points"]) == 1)
        st, out = await get("/series?range=1h&miner=addr.rig1")
        check("GET /series?miner= filters to that miner",
              st.startswith("200") and out["points"][0][3] == 1)
        st, _ = await get("/series?range=forever")
        check("bad range -> 400", st.startswith("400"))
        st, _ = await get("/series?since=yesterday")
        check("non-integer since -> 400", st.startswith("400"))
        st, out = await get("/series/miners")
        check("GET /series/miners lists identities",
              st.startswith("200") and [m["miner"] for m in out["miners"]] == ["addr.rig1"])
        st, _ = await get("/series?miner=" + "x" * 5000)
        check("absurdly long miner key handled (truncated, no error)", st.startswith("200"))
        srv.metrics = None
        st, _ = await get("/series")
        check("no metrics store -> 503, not a crash", st.startswith("503"))
    asyncio.run(run())
    s.close()


def test_load():
    """24 h of 10 s samples for 5 miners: write cost, query cost, file size."""
    c = Clock(T0)
    s = store(c, "load.sqlite")
    miners = [f"addr.rig{i}" for i in range(5)]
    t_write = 0.0
    for i in range(24 * 360):
        ts = T0 + i * 10
        c.t = ts + 1
        s.add_sample(ts, sample(work=50, hr=1e12, acc=2, workers=5),
                     {m: sample(work=10, acc=1, diff=4096) for m in miners})
        if i % 6 == 5:
            t0 = time.perf_counter()
            s.flush()
            t_write += time.perf_counter() - t0
    writes = 24 * 60
    per_write_ms = t_write / writes * 1000
    t0 = time.perf_counter()
    for _ in range(20):
        s.series("1h")
        s.series("24h", miner="addr.rig3")
    per_query_ms = (time.perf_counter() - t0) / 40 * 1000
    s.close()
    size_mb = sum(os.path.getsize(os.path.join(DATA_DIR, f)) for f in os.listdir(DATA_DIR)
                  if f.startswith("load.sqlite")) / 1e6
    print(f"      load: {writes} batched writes, avg {per_write_ms:.1f} ms each; "
          f"query avg {per_query_ms:.1f} ms; file {size_mb:.1f} MB for 24 h x 5 miners")
    check("batched write stays cheap (< 50 ms avg)", per_write_ms < 50)
    check("chart query stays cheap (< 50 ms avg)", per_query_ms < 50)
    check("24 h of 10 s data for 5 miners stays small (< 20 MB)", size_mb < 20)


if __name__ == "__main__":
    test_store()
    test_hours_and_retention()
    test_write_failure()
    test_sampler()
    test_http()
    test_load()
    print(f"\n{ok} passed, {fail} failed")
    sys.exit(0 if fail == 0 else 1)
