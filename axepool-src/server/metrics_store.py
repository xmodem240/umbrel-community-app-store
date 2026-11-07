#!/usr/bin/env python3
"""
AxePool - time-series metrics store for the live dashboard graphs.

One SQLite file (metrics.sqlite under APP_DATA_DIR) at three resolutions:

    s10   10-second samples   kept 24 hours
    m1    1-minute rollups    kept 30 days
    h1    1-hour rollups      kept 365 days

Every row is (ts, miner, ...). miner == "" is the pool total; any other value is
one miner's identity (the Stratum username, address.worker).

Work, not hashrate, is what is stored and summed: `work` is the sum of the
assigned share difficulties accepted in the interval, so any bucket's average
hashrate is exact at every resolution: work * 2**32 / bucket_seconds. `hr_5m` is
the server's smoothed 5-minute figure at sample time, kept for a steady line.

Load on the Umbrel is kept low on purpose:
  * samples are buffered in memory and written in one transaction every
    FLUSH_INTERVAL seconds (6 samples per write at the defaults);
  * rollups and pruning run in that same write;
  * every SQLite call runs on one dedicated worker thread, so a slow disk never
    stalls share processing on the event loop.

A crash loses at most FLUSH_INTERVAL seconds of samples. Standard library only.
"""

import concurrent.futures
import logging
import os
import sqlite3
import threading
import time

log = logging.getLogger("solobch.metrics")

SAMPLE_INTERVAL = 10          # seconds between samples (the s10 resolution)
FLUSH_INTERVAL = 60.0         # seconds between batched writes
PRUNE_INTERVAL = 3600.0       # seconds between retention passes
POOL = ""                     # miner key for the pool-total row
MAX_MINER_KEY = 200           # longest identity stored (Stratum usernames are bounded upstream)

# table -> (bucket seconds, retention seconds)
TABLES = {
    "s10": (SAMPLE_INTERVAL, 24 * 3600),
    "m1": (60, 30 * 24 * 3600),
    "h1": (3600, 365 * 24 * 3600),
}

# range name -> (seconds covered, table to read). Chosen so a chart never gets
# more than ~1440 points: 1h of 10s = 360, 24h of 1m = 1440, 30d of 1h = 720.
RANGES = {
    "1h": (3600, "s10"),
    "6h": (6 * 3600, "m1"),
    "24h": (24 * 3600, "m1"),
    "7d": (7 * 24 * 3600, "h1"),
    "30d": (30 * 24 * 3600, "h1"),
    "1y": (365 * 24 * 3600, "h1"),
}

COLUMNS = ("ts", "miner", "work", "hr_5m", "accepted", "rejected", "difficulty",
           "workers")

SCHEMA = """
CREATE TABLE IF NOT EXISTS {t} (
    ts          INTEGER NOT NULL,   -- bucket start, epoch seconds
    miner       TEXT    NOT NULL,   -- '' = pool total
    work        REAL    NOT NULL,   -- sum of assigned difficulty of accepted shares
    hr_5m       REAL    NOT NULL,   -- smoothed 5-minute hashrate (H/s), averaged in rollups
    accepted    INTEGER NOT NULL,
    rejected    INTEGER NOT NULL,
    difficulty  REAL,               -- share difficulty at the end of the bucket (NULL for pool)
    workers     INTEGER NOT NULL,   -- connected miners (pool row) / 1 (miner row); max in rollups
    PRIMARY KEY (ts, miner)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS {t}_miner_ts ON {t} (miner, ts);
"""


def metrics_path():
    explicit = os.environ.get("SOLOBCH_METRICS")
    if explicit:
        return explicit
    data = os.environ.get("APP_DATA_DIR")
    return os.path.join(data, "metrics.sqlite") if data else ""


def bucket_hashrate(work, seconds):
    """Average hashrate (H/s) of a bucket from its summed share difficulty."""
    return work * (2 ** 32) / seconds if seconds > 0 else 0.0


class MetricsStore:
    def __init__(self, path=None, now=time.time):
        self.path = metrics_path() if path is None else path
        self._now = now
        self._buf = []                         # pending s10 rows (tuples in COLUMNS order)
        self._buf_lock = threading.Lock()
        self._last_flush = now()
        self._last_prune = 0.0
        self._db = None
        self.enabled = bool(self.path)
        # One thread owns the connection; sqlite3 objects never cross threads.
        self._pool = concurrent.futures.ThreadPoolExecutor(
            max_workers=1, thread_name_prefix="metrics-db")
        if self.enabled:
            try:
                self._pool.submit(self._open).result()
            except Exception as e:
                log.warning("metrics store disabled: cannot open %s (%s)", self.path, e)
                self.enabled = False

    # ---- worker-thread side ------------------------------------------------ #
    def _open(self):
        os.makedirs(os.path.dirname(self.path) or ".", exist_ok=True)
        db = sqlite3.connect(self.path, isolation_level=None, check_same_thread=False)
        db.execute("PRAGMA journal_mode=WAL")      # readers never block the writer
        db.execute("PRAGMA synchronous=NORMAL")    # safe with WAL; far fewer fsyncs
        db.execute("PRAGMA busy_timeout=5000")
        for t in TABLES:
            db.executescript(SCHEMA.format(t=t))
        self._db = db

    def _write(self, rows, now):
        db = self._db
        db.execute("BEGIN")
        try:
            if rows:
                db.executemany(
                    "INSERT OR REPLACE INTO s10 VALUES (?,?,?,?,?,?,?,?)", rows)
            self._rollup(db, "s10", "m1", now)
            self._rollup(db, "m1", "h1", now)
            if now - self._last_prune >= PRUNE_INTERVAL:
                for t, (_, keep) in TABLES.items():
                    db.execute(f"DELETE FROM {t} WHERE ts < ?", (int(now - keep),))
                self._last_prune = now
            db.execute("COMMIT")
        except Exception:
            db.execute("ROLLBACK")
            raise

    @staticmethod
    def _rollup(db, src, dst, now):
        """Aggregate every *completed* bucket of `dst` from `src`.

        Starts one bucket before the newest existing `dst` row, so a bucket that
        was rolled up while late samples were still buffered gets recomputed
        (INSERT OR REPLACE). Never touches the bucket still in progress.
        """
        size = TABLES[dst][0]
        done_to = int(now) // size * size          # start of the current, open bucket
        row = db.execute(f"SELECT MAX(ts) FROM {dst}").fetchone()
        start = (row[0] - size) if row and row[0] is not None else 0
        # Difficulty: the value from the newest source row in the bucket.
        db.execute(f"""
            INSERT OR REPLACE INTO {dst}
            SELECT b, miner, SUM(work), AVG(hr_5m), SUM(accepted), SUM(rejected),
                   (SELECT s2.difficulty FROM {src} s2
                     WHERE s2.miner = g.miner AND s2.ts >= g.b AND s2.ts < g.b + ?
                     ORDER BY s2.ts DESC LIMIT 1),
                   MAX(workers)
            FROM (SELECT (ts / ?) * ? AS b, * FROM {src}
                  WHERE ts >= ? AND ts < ?) g
            GROUP BY b, miner
        """, (size, size, size, start, done_to))

    def _query(self, table, miner, start, since):
        cols = ", ".join(COLUMNS)
        lo = max(start, since + 1) if since is not None else start
        cur = self._db.execute(
            f"SELECT {cols} FROM {table} WHERE miner = ? AND ts >= ? ORDER BY ts",
            (miner, int(lo)))
        return cur.fetchall()

    def _miners(self, start):
        cur = self._db.execute(
            "SELECT miner, MAX(ts) FROM m1 WHERE miner != '' AND ts >= ? "
            "GROUP BY miner UNION "
            "SELECT miner, MAX(ts) FROM s10 WHERE miner != '' AND ts >= ? "
            "GROUP BY miner", (int(start), int(start)))
        last = {}
        for m, ts in cur.fetchall():
            last[m] = max(ts, last.get(m, 0))
        return last

    # ---- event-loop side --------------------------------------------------- #
    def add_sample(self, ts, pool, miners):
        """Buffer one sample. `pool` and each value of `miners` (keyed by
        identity) are dicts with work, hr_5m, accepted, rejected, difficulty,
        workers. Cheap: no I/O happens here."""
        if not self.enabled:
            return
        b = int(ts) // SAMPLE_INTERVAL * SAMPLE_INTERVAL
        rows = [_row(b, POOL, pool)]
        for key, m in miners.items():
            if key:
                rows.append(_row(b, str(key)[:MAX_MINER_KEY], m))
        with self._buf_lock:
            self._buf.extend(rows)

    def due(self):
        # Half a sample of slack: the sampler checks this once per 10 s tick,
        # and the previous flush was stamped a few ms after its own tick, so a
        # strict ">= 60" would come up short and slip every batch to 70 s.
        return self.enabled and (
            self._now() - self._last_flush) >= FLUSH_INTERVAL - SAMPLE_INTERVAL / 2

    def flush(self):
        """Write buffered samples, roll up and prune. Blocking; call it from an
        executor (see aflush) unless shutting down. Returns rows written."""
        if not self.enabled:
            return 0
        with self._buf_lock:
            rows, self._buf = self._buf, []
        now = self._now()
        self._last_flush = now
        try:
            self._pool.submit(self._write, rows, now).result()
            return len(rows)
        except Exception as e:
            # Keep the rows for the next attempt rather than dropping them, but
            # bound the buffer so a broken disk cannot grow memory forever.
            log.warning("metrics write failed (%s) - will retry", e)
            with self._buf_lock:
                self._buf = (rows + self._buf)[-int(3600 / SAMPLE_INTERVAL) * 64:]
            return 0

    async def aflush(self):
        import asyncio
        loop = asyncio.get_running_loop()
        return await loop.run_in_executor(None, self.flush)

    def series(self, range_name="1h", miner=POOL, since=None):
        """Points for a chart. Includes rows still in the write buffer for the
        10-second table, so a 5-10 s refresh sees the newest sample at once.
        Returns dict(range, table, bucket, points=[[ts, hashrate, hr_5m,
        accepted, rejected, difficulty, workers], ...])."""
        if range_name not in RANGES:
            raise ValueError(f"range must be one of {', '.join(RANGES)}")
        span, table = RANGES[range_name]
        bucket = TABLES[table][0]
        miner = "" if miner is None else str(miner)[:MAX_MINER_KEY]
        if since is not None:
            since = int(since)
        start = int(self._now()) - span
        rows = []
        if self.enabled:
            rows = self._pool.submit(self._query, table, miner, start, since).result()
            if table == "s10":
                lo = start if since is None else max(start, since + 1)
                with self._buf_lock:
                    pending = [r for r in self._buf if r[1] == miner and r[0] >= lo]
                seen = {r[0] for r in rows}
                rows = sorted(rows + [r for r in pending if r[0] not in seen])
        pts = [[r[0], round(bucket_hashrate(r[2], bucket), 2), round(r[3], 2),
                r[4], r[5], r[6], r[7]] for r in rows]
        return {"range": range_name, "table": table, "bucket": bucket,
                "fields": ["ts", "hashrate", "hr_5m", "accepted", "rejected",
                           "difficulty", "workers"],
                "points": pts}

    def miners(self, days=30):
        """Identities seen in the last `days`, with their last-seen time."""
        if not self.enabled:
            return {}
        start = self._now() - days * 86400
        last = self._pool.submit(self._miners, start).result()
        with self._buf_lock:
            for r in self._buf:
                if r[1]:
                    last[r[1]] = max(r[0], last.get(r[1], 0))
        return last

    def close(self):
        if self.enabled:
            try:
                self.flush()
            finally:
                db = self._db
                if db is not None:
                    self._pool.submit(db.close).result()
                    self._db = None
        self._pool.shutdown(wait=True)
        self.enabled = False


def _row(ts, key, d):
    return (int(ts), key, float(d.get("work", 0.0)), float(d.get("hr_5m", 0.0)),
            int(d.get("accepted", 0)), int(d.get("rejected", 0)),
            (float(d["difficulty"]) if d.get("difficulty") is not None else None),
            int(d.get("workers", 0)))
