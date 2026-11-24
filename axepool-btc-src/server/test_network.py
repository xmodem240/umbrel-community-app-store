#!/usr/bin/env python3
"""
AxePool BTC - payout addresses must match the node's Bitcoin network.

Drives the real mining.authorize path: every supported address type is accepted
on mainnet and pays exactly Bitcoin Core's scriptPubKey; another network's
address, a BCH CashAddr and an anyone-can-spend witness version are refused;
once the node reports its chain, that chain wins over the configured network;
the network setting is validated in Settings and on load.

Offline:   cd server && python3 test_network.py
"""
import asyncio
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import test_pipeline as tp                       # noqa: E402  (sets APP_DATA_DIR, harness)
import config                                    # noqa: E402
import status                                    # noqa: E402
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


# (address, Bitcoin Core's scriptPubKey) from src/test/data/key_io_valid.json
with open(os.path.join(HERE, "fixtures", "bitcoin_core_key_io_valid.json")) as f:
    VECTORS = {e[0]: (e[1], e[2]["chain"]) for e in json.load(f)}


def vec(chain, prefix):
    return next(a for a, (spk, c) in VECTORS.items() if c == chain and a.startswith(prefix)
                and (len(spk) == 44 or not prefix.endswith("1q")))


async def main():
    jm = JobManager(tp.FakeRPC(), 1, stats=Stats())
    jm.vardiff = {"enabled": False, "target_spm": 20, "min": 1, "max": 4000000}
    jm._new_real(tp.make_gbt(950_000, "22" * 32, 312_512_345), clean=True)
    jm.mode = "real"
    server = stratum.StratumServer(jm)

    async def login(user):
        w = tp.FakeWriter("10.0.0.7")
        c = stratum.ClientConn(server, None, w)
        server.clients.add(c)
        await c.on_subscribe(1, ["bitaxe/test"])
        w.messages()
        await c.on_authorize(2, [user, "x"])
        reply = next(m for m in w.messages() if m.get("id") == 2)
        return c, reply

    check("payout network defaults to mainnet before the node answers",
          jm.chain is None and jm.payout_network == "main")

    main_cases = [a for a, (spk, c) in VECTORS.items() if c == "main"]
    for addr in main_cases:
        spk = bytes.fromhex(VECTORS[addr][0])
        c, r = await login(f"{addr}.rig")
        safe = not (0x51 <= spk[0] <= 0x60) or (spk[0] == 0x51 and spk[1] == 32)
        if safe:
            check(f"mainnet {addr[:14]}… accepted, pays Core's script",
                  r["result"] is True and c.payout_script == spk)
        else:
            check(f"anyone-can-spend {addr[:14]}… refused (error 24)",
                  r["result"] is False and r["error"][0] == 24 and c.payout_script is None)

    for label, addr in (("testnet4 bech32", vec("testnet4", "tb1q")),
                        ("regtest bech32", vec("regtest", "bcrt1q")),
                        ("testnet legacy", vec("testnet4", "m")),
                        ("BCH CashAddr", "bitcoincash:qr95sy3j9xwd2ap32xkykttr4cvcu7as4y0qverfuy"),
                        ("BCH CashAddr without prefix", "qr95sy3j9xwd2ap32xkykttr4cvcu7as4y0qverfuy"),
                        ("mistyped bc1q (bad checksum)",
                         vec("main", "bc1q")[:-1] + ("q" if vec("main", "bc1q")[-1] != "q" else "p")),
                        ("empty", "")):
        c, r = await login(f"{addr}.rig")
        check(f"{label} refused on mainnet with a reason",
              r["result"] is False and r["error"][0] == 24
              and "Invalid Bitcoin payout address" in r["error"][1] and not c.authorized)

    # ---- the node reports its chain: that wins over the configured network ----
    class ChainRPC(tp.FakeRPC):
        def __init__(self, chain):
            super().__init__()
            self.chain = chain

        def getblockchaininfo(self):
            return {"chain": self.chain, "blocks": 1, "headers": 1,
                    "initialblockdownload": True, "bestblockhash": None}

    jm.rpc = ChainRPC("regtest")
    await jm._poll(asyncio.get_running_loop())
    check("node's chain is picked up", jm.chain == "regtest" and jm.payout_network == "regtest")
    c, r = await login(f"{vec('regtest', 'bcrt1q')}.rig")
    check("regtest address accepted once the node says regtest", r["result"] is True)
    c, r = await login(f"{vec('regtest', 'm')}.rig")
    check("legacy m... accepted on regtest (same version bytes as Core uses)",
          r["result"] is True)
    c, r = await login(f"{vec('main', 'bc1q')}.rig")
    check("mainnet address refused on a regtest node", r["result"] is False)
    check("status snapshot reports the network", jm.status_snapshot()["network"] == "regtest")

    jm.rpc = ChainRPC("not-a-chain")
    await jm._poll(asyncio.get_running_loop())
    check("an unknown chain name does not replace the known one", jm.chain == "regtest")

    # ---- the setting ---------------------------------------------------------
    j2 = JobManager(tp.FakeRPC(), 1, stats=Stats(), network="testnet4")
    check("configured testnet4 (no node yet) checks tb1 addresses",
          j2.payout_network == "testnet4")
    check("Umbrel's 'mainnet' maps to main", JobManager(tp.FakeRPC(), 1, stats=Stats(),
                                                        network="mainnet").payout_network == "main")
    cur = config.load()
    for good in ("mainnet", "testnet4", "signet", "regtest", "main"):
        check(f"Settings accepts network {good}",
              status.validate_updates({"bitcoin_network": good}, cur) is None)
    for bad in ("bch", "", 5, "liquid"):
        check(f"Settings refuses network {bad!r}",
              status.validate_updates({"bitcoin_network": bad}, cur) is not None)
    os.environ["SOLOBCH_NETWORK"] = "signet"
    check("SOLOBCH_NETWORK (Umbrel's APP_BITCOIN_NETWORK) sets it",
          config.load()["bitcoin_network"] == "signet"
          and "bitcoin_network" in config.env_locked())
    os.environ["SOLOBCH_NETWORK"] = "dogecoin"
    check("an unknown network value falls back to mainnet",
          config.load()["bitcoin_network"] == "mainnet")
    del os.environ["SOLOBCH_NETWORK"]


if __name__ == "__main__":
    asyncio.run(main())
    print(f"\n{ok} passed, {fail} failed")
    sys.exit(1 if fail else 0)
