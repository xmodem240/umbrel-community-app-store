#!/usr/bin/env python3
"""
AxePool BTC vs a real Bitcoin Core on regtest (release check; not run in CI).

    BITCOIND=/path/to/bitcoind python3 tools/regtest_proof.py

Starts its own throwaway regtest node and app instance on ports 18543 / 13334 /
13335, deletes nothing of yours. Last run: Bitcoin Core v32.1, 35 passed.

1. Node: start bitcoind -regtest, mine 110 blocks to a P2WPKH key we hold
   (private key 1, so Core signs with signrawtransactionwithkey; no wallet).
2. Mempool: SegWit spends (P2WPKH inputs), outputs to P2WPKH / P2PKH / P2TR,
   plus a child spending an unconfirmed parent (in-block dependency).
3. Addresses: Core's validateaddress scriptPubKey == btcaddr.to_script for
   wpkh / pkh / sh-wpkh / wsh / tr regtest addresses.
4. Adversarial block assembly (getblocktemplate proposal mode, nothing
   broadcast): AxePool's correct block must be valid; missing commitment,
   wrong commitment, coinbase not in witness form, wtxid merkle must fail.
5. Real server: run axepool-btc-src/server/stratum.py against the node; a
   Stratum client that builds headers only from mining.notify (like firmware)
   mines blocks through it. Each block must be ACCEPTED by the node, pay the
   miner's address, carry the witness commitment + reserved value, include
   every mempool tx, and carry the coinbase tag; a tag changed via POST /config
   must appear in the next block. A mainnet address must be refused.
6. The app's own "Is this working?" checks (/diag) on the regtest node.
"""
import base64
import hashlib
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

REPO = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "server")
BITCOIND = os.environ.get("BITCOIND", "bitcoind")   # a Bitcoin Core (or Knots) binary
sys.path.insert(0, REPO)
import btcaddr                                    # noqa: E402
from template import BlockTemplate                # noqa: E402
from coinbase import build_coinbase_parts, assemble, with_witness   # noqa: E402
from block import serialize_block                 # noqa: E402
from merkle import merkle_root                    # noqa: E402

RPC_PORT, STRATUM_PORT, STATUS_PORT = 18543, 13334, 13335
G = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"   # pubkey of privkey 1

ok = fail = 0


def check(label, cond, detail=""):
    global ok, fail
    if cond:
        ok += 1
        print(f"PASS  {label}")
    else:
        fail += 1
        print(f"FAIL  {label}  {detail}")


def sha256d(b):
    return hashlib.sha256(hashlib.sha256(b).digest()).digest()


def b58check(payload):
    B58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
    raw = payload + sha256d(payload)[:4]
    n = int.from_bytes(raw, "big")
    out = ""
    while n:
        n, r = divmod(n, 58)
        out = B58[r] + out
    return "1" * (len(raw) - len(raw.lstrip(b"\0"))) + out


WIF = b58check(b"\xef" + (1).to_bytes(32, "big") + b"\x01")   # regtest, compressed


def rpc(method, *params):
    body = json.dumps({"jsonrpc": "1.0", "id": 1, "method": method,
                       "params": list(params)}).encode()
    req = urllib.request.Request(f"http://127.0.0.1:{RPC_PORT}/", data=body,
                                 headers={"Authorization": "Basic " +
                                          base64.b64encode(b"u:p").decode()})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            out = json.load(r)
    except urllib.error.HTTPError as e:
        out = json.load(e)
    if out.get("error"):
        raise RuntimeError(f"{method}: {out['error']}")
    return out["result"]


def addr(desc):
    d = rpc("getdescriptorinfo", desc)["descriptor"]
    return rpc("deriveaddresses", d)[0]


def on_tip(n):
    """True when mining.notify n builds on the node's current tip."""
    prev = b"".join(bytes.fromhex(n[1])[i:i + 4][::-1] for i in range(0, 32, 4))
    return prev[::-1].hex() == rpc("getbestblockhash")


def http(method, path, body=None):
    req = urllib.request.Request(f"http://127.0.0.1:{STATUS_PORT}{path}", method=method,
                                 data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


# --------------------------------------------------------------------------- #
class Miner:
    """Stratum V1 client that, like ESP-Miner, builds every header from the
    mining.notify fields only."""

    def __init__(self, user, agent="bitaxe/BM1370/v2.15.4"):
        self.s = socket.create_connection(("127.0.0.1", STRATUM_PORT), timeout=60)
        self.buf = b""
        self.id = 0
        self.notify = None
        self.mask = None
        self.replies = {}
        self.send("mining.configure", [["version-rolling"],
                                       {"version-rolling.mask": "1fffe000",
                                        "version-rolling.min-bit-count": 13}])
        r = self.wait_reply(self.id)
        self.mask = int(r["result"]["version-rolling.mask"], 16)
        self.send("mining.subscribe", [agent])
        r = self.wait_reply(self.id)
        self.en1, self.en2size = r["result"][1], r["result"][2]
        self.send("mining.authorize", [user, "x"])
        self.auth = self.wait_reply(self.id)

    def send(self, method, params):
        self.id += 1
        self.s.sendall((json.dumps({"id": self.id, "method": method,
                                    "params": params}) + "\n").encode())

    def _read(self):
        while b"\n" not in self.buf:
            chunk = self.s.recv(65536)
            if not chunk:
                raise ConnectionError("pool closed the connection")
            self.buf += chunk
        line, self.buf = self.buf.split(b"\n", 1)
        m = json.loads(line)
        if m.get("method") == "mining.notify":
            self.notify = m["params"]
        elif m.get("id") is not None:
            self.replies[m["id"]] = m
        return m

    def wait_reply(self, mid):
        while mid not in self.replies:
            self._read()
        return self.replies.pop(mid)

    def wait_notify(self, pred, timeout=90):
        end = time.time() + timeout
        self.s.settimeout(2)
        while time.time() < end:
            if self.notify and pred(self.notify):
                return self.notify
            try:
                self._read()
            except (socket.timeout, TimeoutError):
                pass
        raise RuntimeError("no matching job")

    def mine(self, n):
        """Find a header under the job's own nBits (regtest: ~1 in 2) and submit."""
        job, prev_n, c1, c2, branch, ver, nbits, ntime, _ = n
        prev = b"".join(bytes.fromhex(prev_n)[i:i + 4][::-1] for i in range(0, 32, 4))
        bits = int(nbits, 16)
        target = (bits & 0x7fffff) << (8 * ((bits >> 24) - 3))
        en2 = os.urandom(self.en2size).hex()
        cb = bytes.fromhex(c1 + self.en1 + en2 + c2)
        root = sha256d(cb)
        for h in branch:
            root = sha256d(root + bytes.fromhex(h))
        rolled = 0x00002000                      # use one rolled version bit
        version = (int(ver, 16) & ~self.mask) | (rolled & self.mask)
        for nonce in range(1 << 20):
            hdr = (version.to_bytes(4, "little") + prev + root + int(ntime, 16).to_bytes(4, "little")
                   + bits.to_bytes(4, "little") + nonce.to_bytes(4, "little"))
            if int.from_bytes(sha256d(hdr), "little") <= target:
                break
        self.s.settimeout(60)
        self.send("mining.submit", [self.user_name, job, en2, ntime, f"{nonce:08x}",
                                    f"{rolled & self.mask:08x}"])
        r = self.wait_reply(self.id)
        return r, sha256d(hdr)[::-1].hex(), cb

    user_name = None


def main():
    datadir = tempfile.mkdtemp(prefix="btcreg-")
    appdata = tempfile.mkdtemp(prefix="axepool-btc-data-")
    node = subprocess.Popen([BITCOIND, "-regtest", f"-datadir={datadir}", "-server",
                             "-rpcuser=u", "-rpcpassword=p", f"-rpcport={RPC_PORT}",
                             "-listen=0", "-fallbackfee=0.0001", "-printtoconsole=0"])
    server = None
    try:
        for _ in range(60):
            try:
                rpc("getblockchaininfo")
                break
            except Exception:
                time.sleep(0.5)
        print("node:", rpc("getnetworkinfo")["subversion"], rpc("getblockchaininfo")["chain"])

        a_wpkh = addr(f"wpkh({G})")
        a_pkh = addr(f"pkh({G})")
        a_shwpkh = addr(f"sh(wpkh({G}))")
        a_wsh = addr(f"wsh(pk({G}))")
        a_tr = addr(f"tr({G})")
        # ---- 3. addresses vs Core -----------------------------------------
        for a in (a_wpkh, a_pkh, a_shwpkh, a_wsh, a_tr):
            core = bytes.fromhex(rpc("validateaddress", a)["scriptPubKey"])
            check(f"btcaddr({a[:12]}…) == Core's scriptPubKey",
                  btcaddr.to_script(a, "regtest") == core)
        check("our WIF is the key behind the wpkh address",
              rpc("getdescriptorinfo", f"wpkh({WIF})")["descriptor"].startswith(f"wpkh({G})"))

        # ---- 1. chain + 2. mempool ----------------------------------------
        rpc("generatetoaddress", 110, a_wpkh)
        spk_w = rpc("validateaddress", a_wpkh)["scriptPubKey"]
        txids = []
        for h in range(1, 7):
            cb = rpc("getblock", rpc("getblockhash", h), 2)["tx"][0]
            outs = [{a_wpkh: 20}, {a_pkh: 10}, {a_tr: 10}, {a_wsh: 9.999}]
            raw = rpc("createrawtransaction", [{"txid": cb["txid"], "vout": 0}], outs)
            prev = [{"txid": cb["txid"], "vout": 0, "scriptPubKey": spk_w,
                     "amount": cb["vout"][0]["value"]}]
            signed = rpc("signrawtransactionwithkey", raw, [WIF], prev)
            assert signed["complete"], signed
            txids.append(rpc("sendrawtransaction", signed["hex"]))
        parent = rpc("getrawtransaction", txids[0], True)
        raw = rpc("createrawtransaction", [{"txid": txids[0], "vout": 0}], [{a_wpkh: 19.99}])
        signed = rpc("signrawtransactionwithkey", raw, [WIF],
                     [{"txid": txids[0], "vout": 0, "scriptPubKey": spk_w, "amount": 20}])
        txids.append(rpc("sendrawtransaction", signed["hex"]))
        mempool = rpc("getrawmempool")
        check("mempool holds 7 SegWit txs incl. a child of an unconfirmed parent",
              len(mempool) == 7 and parent["hash"] != parent["txid"])

        # ---- 4. adversarial proposals ---------------------------------------
        gbt = rpc("getblocktemplate", {"rules": ["segwit"]})
        check("template has a default_witness_commitment and 7 txs",
              bool(gbt.get("default_witness_commitment")) and len(gbt["transactions"]) == 7)
        t = BlockTemplate(gbt, "prop")
        payout = btcaddr.to_script(a_tr, "regtest")
        c1, c2 = t.coinb1_coinb2(payout, 4, 4, tag=b"/AxePool/")
        cbraw, cbhash = t.coinbase(c1, "00000000", "00000001", c2)
        hdr = t.header(cbhash, t.version, t.curtime, 0)

        def propose(blockhex):
            return rpc("getblocktemplate", {"mode": "proposal", "data": blockhex,
                                            "rules": ["segwit"]})
        res = propose(t.full_block(cbraw, hdr))
        check("AxePool's block: proposal VALID", res is None, res)

        # (a) coinbase without the commitment output, txs still carry witnesses
        a1, a2 = build_coinbase_parts(t.height, t.coinbasevalue, payout, 4, 4, tag=b"/AxePool/")
        raw_a = assemble(a1, "00000000", "00000001", a2)
        hdr_a = t.header(sha256d(raw_a), t.version, t.curtime, 0)
        res = propose(serialize_block(hdr_a, raw_a, t.tx_data))
        check("no witness commitment -> rejected", res not in (None, "inconclusive"), res)
        print("      node said:", res)

        # (b) wrong commitment (one byte flipped)
        bad = bytearray(t.witness_commitment); bad[-1] ^= 1
        b1, b2 = build_coinbase_parts(t.height, t.coinbasevalue, payout, 4, 4,
                                      tag=b"/AxePool/", witness_commitment=bytes(bad))
        raw_b = assemble(b1, "00000000", "00000001", b2)
        hdr_b = t.header(sha256d(raw_b), t.version, t.curtime, 0)
        res = propose(serialize_block(hdr_b, with_witness(raw_b), t.tx_data))
        check("wrong witness commitment -> rejected", res not in (None, "inconclusive"), res)
        print("      node said:", res)

        # (c) right commitment, coinbase NOT in witness form (no reserved value)
        res = propose(serialize_block(hdr, cbraw, t.tx_data))
        check("coinbase without witness reserved value -> rejected",
              res not in (None, "inconclusive"), res)
        print("      node said:", res)

        # (d) merkle over wtxids instead of txids
        wt = BlockTemplate(dict(gbt, transactions=[dict(x, txid=x["hash"])
                                                   for x in gbt["transactions"]]), "w")
        hdr_d = wt.header(cbhash, t.version, t.curtime, 0)
        res = propose(t.full_block(cbraw, hdr_d))
        check("merkle over wtxids -> rejected", res not in (None, "inconclusive"), res)
        print("      node said:", res)
        check("template's txid != hash for SegWit txs (so (d) is a real difference)",
              any(x["txid"] != x["hash"] for x in gbt["transactions"]))

        # ---- 5. the real server ---------------------------------------------
        env = dict(os.environ, APP_DATA_DIR=appdata, BCHN_RPC_HOST="127.0.0.1",
                   BCHN_RPC_PORT=str(RPC_PORT), BCHN_RPC_USER="u", BCHN_RPC_PASSWORD="p",
                   SOLOBCH_ZMQ="0", SOLOBCH_NETWORK="regtest",
                   SOLOBCH_STRATUM_PORT=str(STRATUM_PORT), SOLOBCH_STATUS_PORT=str(STATUS_PORT),
                   PYTHONUNBUFFERED="1")
        log = open(os.path.join(appdata, "server.log"), "w")
        server = subprocess.Popen([sys.executable, os.path.join(REPO, "stratum.py")],
                                  env=env, stdout=log, stderr=subprocess.STDOUT,
                                  cwd=appdata)
        for _ in range(60):
            try:
                st = http("GET", "/status")
                if st.get("node", {}).get("mode") == "real" or st.get("mode") == "real":
                    break
            except Exception:
                pass
            time.sleep(1)
        st = http("GET", "/status")
        print("status:", json.dumps({k: st.get("node", st).get(k) for k in
                                     ("mode", "network", "blocks", "ibd")}))

        bad_m = Miner(f"{btcaddr.NETWORKS and 'bc1qvyq0cc6rahyvsazfdje0twl7ez82ndmuac2lhv'}.rig")
        check("mainnet address refused on the regtest node",
              bad_m.auth.get("result") is False, bad_m.auth)

        m = Miner(f"{a_wpkh}.rig1")
        m.user_name = f"{a_wpkh}.rig1"
        check("regtest bech32 payout authorized", m.auth.get("result") is True, m.auth)
        tip0 = rpc("getbestblockhash")
        n = m.wait_notify(lambda n: len(n[4]) >= 3 and on_tip(n))   # real job, 7 txs
        r, bhash, cb = m.mine(n)
        check("submit answered OK", r.get("result") is True, r)
        time.sleep(2)
        tip1 = rpc("getbestblockhash")
        check("node's tip is the block the miner found", tip1 == bhash and tip1 != tip0,
              f"{tip1} vs {bhash}")
        blk = rpc("getblock", tip1, 2)
        cbt = blk["tx"][0]
        check("block includes all 7 mempool txs (8 total)", len(blk["tx"]) == 8, len(blk["tx"]))
        check("mempool is now empty", rpc("getrawmempool") == [])
        check("coinbase output 0 pays the miner's bcrt1q script",
              cbt["vout"][0]["scriptPubKey"]["hex"] == spk_w)
        check("coinbase output 1 = witness commitment, value 0",
              cbt["vout"][1]["value"] == 0 and
              cbt["vout"][1]["scriptPubKey"]["hex"] == gbt["default_witness_commitment"]
              or cbt["vout"][1]["scriptPubKey"]["hex"].startswith("6a24aa21a9ed"))
        check("coinbase witness = [32 zero bytes]",
              cbt["vin"][0].get("txinwitness") == ["00" * 32], cbt["vin"][0])
        check("coinbase scriptSig ends with /AxePool/",
              bytes.fromhex(cbt["vin"][0]["coinbase"]).endswith(b"/AxePool/"))
        check("coinbase txid == sha256d of the coinbase the miner hashed",
              cbt["txid"] == sha256d(cb)[::-1].hex())
        check("block reward = subsidy + fees",
              abs(cbt["vout"][0]["value"] - (50 + 6 * 0.001 + 0.01)) < 1e-8,
              cbt["vout"][0]["value"])

        # ---- tag change via Settings, then an empty-mempool block -----------
        out = http("POST", "/config", {"coinbase_tag": "/Kyle BTC test/"})
        check("POST /config coinbase_tag ok", out.get("ok") is True, out)
        n = m.wait_notify(lambda n: bytes.fromhex(n[3]).startswith(b"/Kyle BTC test/")
                          and on_tip(n), 90)
        r, bhash, cb = m.mine(n)
        time.sleep(2)
        blk = rpc("getblock", rpc("getbestblockhash"), 2)
        cbt = blk["tx"][0]
        check("2nd block (empty mempool) accepted by the node", blk["hash"] == bhash)
        check("...carries the new tag /Kyle BTC test/",
              bytes.fromhex(cbt["vin"][0]["coinbase"]).endswith(b"/Kyle BTC test/"))
        has_commit = len(cbt["vout"]) == 2
        print("      empty block coinbase outputs:", len(cbt["vout"]),
              "witness:", cbt["vin"][0].get("txinwitness"))
        check("...coinbase witness matches whether a commitment is present",
              (cbt["vin"][0].get("txinwitness") == ["00" * 32]) == has_commit)

        # ---- Taproot and legacy payouts ---------------------------------------
        for label, a in (("bcrt1p (Taproot)", a_tr), ("legacy m…", a_pkh),
                         ("P2SH 2…", a_shwpkh)):
            mm = Miner(f"{a}.x")
            mm.user_name = f"{a}.x"
            tip = rpc("getbestblockhash")
            n = mm.wait_notify(on_tip)
            r, bhash, cb = mm.mine(n)
            time.sleep(2)
            blk = rpc("getblock", rpc("getbestblockhash"), 2)
            check(f"{label} payout: block accepted, pays Core's script for it",
                  blk["hash"] == bhash and blk["tx"][0]["vout"][0]["scriptPubKey"]["hex"]
                  == rpc("validateaddress", a)["scriptPubKey"])
            mm.s.close()

        # ---- 6. the app's own diagnostics -----------------------------------
        http("POST", "/diag/start", {})
        snap = {}
        for _ in range(40):
            snap = http("GET", "/diag")
            names = {c["name"]: c["status"] for c in snap.get("checks", [])}
            if any(s in ("fail",) for s in names.values()) or "share" in " ".join(
                    k for k, v in names.items() if v in ("running",)):
                break
            if names and all(v not in ("pending", "running") for v in names.values()):
                break
            time.sleep(2)
        for c in snap.get("checks", []):
            print(f"      diag: {c['status']:7} {c['name']}: {c.get('detail', '')[:110]}")
        names = {c["name"]: c["status"] for c in snap.get("checks", [])}
        assembly = [k for k in names if "assembl" in k.lower() or "propos" in k.lower()
                    or "block" in k.lower()]
        check("diagnostics: block-assembly check passes on Bitcoin Core",
              any(names[k] == "pass" for k in assembly), names)
        http("POST", "/diag/stop", {})
        m.s.close()
        with open(os.path.join(appdata, "server.log")) as f:
            lg = f.read()
        print("      server log BLOCK lines:",
              sum("BTC BLOCK ACCEPTED" in l for l in lg.splitlines()))
        check("server log: 5 blocks accepted, no errors/tracebacks",
              lg.count("BTC BLOCK ACCEPTED") == 5 and "Traceback" not in lg,
              [l for l in lg.splitlines() if "ERROR" in l or "Traceback" in l][:5])
    finally:
        if server:
            server.send_signal(signal.SIGTERM)
            server.wait(10)
        try:
            rpc("stop")
        except Exception:
            pass
        node.wait(30)
        print("app data / server log:", appdata)
    print(f"\n{ok} passed, {fail} failed")
    return fail == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
