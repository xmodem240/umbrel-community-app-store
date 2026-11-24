#!/usr/bin/env python3
"""
AxePool BTC - block-assembly validation via getblocktemplate PROPOSAL mode.

Run this once the node is SYNCED (not IBD). It pulls a real template, assembles a
full candidate block with our code, and asks the node to validate it -- WITHOUT
needing to actually find a block. This proves coinbase (incl. the SegWit
witness commitment), merkle, witness serialization and the getblocktemplate
txid byte order (GBT_TXID_IS_DISPLAY) are consensus-correct against the chain.

    export BCHN_RPC_PASSWORD=...             # the Bitcoin node's RPC password
    python3 proposal_test.py                 # uses a placeholder payout
    BTC_TEST_PAYOUT=bc1q... python3 proposal_test.py

Any valid payout for the node's network works (nothing is broadcast).
"""

import os
import sys

from bch_rpc import from_env, RPCError
from btcaddr import to_script
from diagnostics import DIAG_ADDRESSES
from template import BlockTemplate, GBT_TXID_IS_DISPLAY


def main():
    rpc = from_env()
    info = rpc.getblockchaininfo()
    if info.get("initialblockdownload"):
        print(f"Node still syncing ({info.get('verificationprogress', 0) * 100:.2f}%). "
              "Proposal test needs a fully-synced node — try again later.")
        return 2

    chain = info.get("chain", "main")
    payout = os.environ.get("BTC_TEST_PAYOUT") or DIAG_ADDRESSES[chain]
    gbt = rpc.getblocktemplate()
    t = BlockTemplate(gbt, "propos01")
    script = to_script(payout, chain)

    en1, en2 = "00000000", "00000000"
    c1, c2 = t.coinb1_coinb2(script, 4, 4, tag=b"/AxePool/")
    coinbase_raw, cb_hash = t.coinbase(c1, en1, en2, c2)
    header = t.header(cb_hash, t.version, t.curtime, 0)   # nonce 0 (PoW not checked)
    blockhex = t.full_block(coinbase_raw, header)

    print(f"chain={chain}  height={t.height}  txs={len(t.tx_data)}  "
          f"coinbasevalue={t.coinbasevalue} sat  "
          f"witness_commitment={'yes' if t.witness_commitment else 'no'}")
    print(f"GBT_TXID_IS_DISPLAY={GBT_TXID_IS_DISPLAY}  block={len(blockhex) // 2} bytes")
    print("submitting as proposal (nothing is broadcast)...\n")

    try:
        res = rpc.call("getblocktemplate", [{"mode": "proposal", "data": blockhex}])
    except RPCError as e:
        print(f"proposal RPC error: {e}")
        return 1

    if res in (None, "", "high-hash", "inconclusive"):
        print(f"✅ PROPOSAL VALID (node returned {res!r})")
        print("   Block assembly + merkle + txid byte order are consensus-correct.")
        return 0

    print(f"❌ PROPOSAL REJECTED: {res!r}")
    if res == "bad-txnmrklroot":
        print("   -> merkle/txid order mismatch: flip GBT_TXID_IS_DISPLAY in template.py, retry.")
    elif "cb" in str(res) or "height" in str(res):
        print("   -> coinbase issue (BIP34 height / value / script).")
    return 1


if __name__ == "__main__":
    sys.exit(main())
