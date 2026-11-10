#!/usr/bin/env python3
"""
AxePool - monitoring tools are not miners.

Bitcoin Cash Lab's Stratum Race logs in like a miner (user agent
"bitcoin-cash-lab/1.0", a burn address, worker "worker1") to time new jobs and
never submits a share. It must still receive jobs, but must not be counted,
graphed, polled, retargeted or alerted on. Anything that submits a valid share
is a miner, whatever it calls itself.

Offline:   cd server && python3 test_monitor.py
"""
import asyncio
import os
import sys
import time
import weakref

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import test_pipeline as tp                       # noqa: E402
import stratum                                   # noqa: E402
import status                                    # noqa: E402
from stratum import monitor_name                 # noqa: E402
from job_manager import JobManager               # noqa: E402
from miner_poller import MinerPoller             # noqa: E402
from stats import Stats                          # noqa: E402

ok = fail = 0
LAB_UA = "bitcoin-cash-lab/1.0"
BURN = "1BitcoinEaterAddressDontSendf59kuE"      # the login Bitcoin Cash Lab uses


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


async def connect(server, ip, ua, user):
    w = tp.FakeWriter(ip)
    c = stratum.ClientConn(server, None, w)
    server.clients.add(c)
    await c.on_subscribe(1, [ua])
    await c.on_authorize(2, [user, "x"])
    m = tp.Miner(c, w)
    m.absorb()
    return c, w, m


async def main():
    check("user agent matching",
          monitor_name(LAB_UA) == "Bitcoin Cash Lab"
          and monitor_name("Bitcoin-Cash-Lab/2.3 ") == "Bitcoin Cash Lab"
          and all(monitor_name(x) is None for x in
                  ("bitaxe/BM1370/v2.15.4", "NerdQAxe++", "", None, 5,
                   "my-bitcoin-cash-lab/1.0", "bitcoin-cash-lab")))

    jm = JobManager(tp.FakeRPC(), 1, stats=Stats())
    jm.vardiff = {"enabled": True, "target_spm": 20, "min": 1, "max": 4000000}
    jm._new_real(tp.make_gbt(950_000, "22" * 32, 312_512_345), clean=True)
    jm.mode = "real"
    server = stratum.StratumServer(jm)
    lab, lw, lm = await connect(server, "10.21.0.9", LAB_UA, f"{BURN}.worker1")
    rig, rw, rm = await connect(server, "192.168.50.74", "bitaxe/BM1370/v2.15.4",
                                f"{tp.ADDR_A}.naja")

    check("Lab connection is flagged, the Bitaxe is not",
          lab.monitor == "Bitcoin Cash Lab" and rig.monitor is None)
    check("Lab still authorized and still received a job (its timing keeps working)",
          lab.authorized and lm.notify is not None)
    jm._new_real(tp.make_gbt(950_001, "33" * 32, 312_500_000), clean=True)
    await lab.send_current_job()
    before = lm.notify[0]
    lm.absorb()
    check("Lab receives new jobs when the tip changes", lm.notify[0] != before)

    snap = status.build_snapshot(server, jm)
    check("/status miners lists only the real miner",
          [m["worker"] for m in snap["miners"]] == ["naja"])
    check("/status monitors lists the Lab connection by name",
          [(m["name"], m["ip"]) for m in snap["monitors"]] == [("Bitcoin Cash Lab", "10.21.0.9")])
    check("worker count excludes the monitor", snap["pool"]["workers"] == 1)
    check("Prometheus worker gauge excludes the monitor",
          "solobch_workers 1" in status.build_metrics(server, jm))

    pool, miners = stratum.collect_sample([lab, rig], 0.0, weakref.WeakKeyDictionary(),
                                          time.monotonic())
    check("graphs: monitor not sampled, miners-connected is 1",
          list(miners) == [rig.raw_user] and pool["workers"] == 1)

    p = MinerPoller(fetch=lambda ip, port: {})
    p.note_connections([lab, rig])
    check("miner API poller never targets the monitor's address",
          list(p.targets) == ["192.168.50.74:80"])
    p.close()

    check("offline alerts: only the real miner is tracked",
          list(server.seen_miners) == [rig.raw_user])

    await lab.set_difficulty(64)                 # above the minimum, so a retarget would lower it
    lab.connected_mono -= 600
    lab._last_vardiff -= 600
    lab._diff_changed -= 600
    d0, sent0 = lab.difficulty, len(lw.buf)
    await lab.maybe_retarget()
    check("vardiff leaves a monitor alone (no pointless difficulty messages)",
          lab.difficulty == d0 and len(lw.buf) == sent0)
    check("difficulty seeding skips a monitor",
          await lab.maybe_seed({"state": "mining", "hashrate_10m_ghs": 7000.0}) is None
          and lab.difficulty == d0)

    # Anything that really hashes is a miner, whatever its user agent says.
    await lab.set_difficulty(1)
    await lab.send_current_job()
    lm.absorb()
    params, *_ = lm.find(lambda h: SCALE // 64 < h <= SCALE, notify=list(lm.notify))
    r = await lm.submit(params)
    snap = status.build_snapshot(server, jm)
    check("a 'monitor' that submits a valid share becomes a normal miner",
          r.get("result") is True and lab.monitor is None and snap["monitors"] == []
          and sorted(m["worker"] for m in snap["miners"]) == ["naja", "worker1"]
          and lab.raw_user in server.seen_miners)

    if server.submit_tasks:
        await asyncio.gather(*list(server.submit_tasks))
    print(f"\n{ok} passed, {fail} failed")
    return fail == 0


if __name__ == "__main__":
    sys.exit(0 if asyncio.run(main()) else 1)
