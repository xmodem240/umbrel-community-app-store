#!/usr/bin/env python3
"""
AxePool BTC - Bitcoin coinbase transaction builder.

Builds a Bitcoin coinbase tx split into Stratum coinb1/coinb2 halves, with
the extranonce inserted between them:

    full coinbase = coinb1 + extranonce1 + extranonce2 + coinb2

scriptSig layout = <BIP34 height push> + extranonce1 + extranonce2 + <tag>

SegWit (BIP141): the coinbase the miner hashes (and whose sha256d is the txid
in the merkle tree) is the plain, non-witness serialization. It carries a second,
zero-value output: the witness commitment script the node hands out as
getblocktemplate's `default_witness_commitment`. Only the block sent to
submitblock carries the coinbase in witness form (marker 0x00, flag 0x01 and a
single 32-byte all-zero witness reserved value): see with_witness(). Output 0
pays the miner's payout script the full `coinbasevalue` (subsidy + fees).
Standard library only.

Run directly for the self-test (offline, no node):  python3 coinbase.py
"""


def encode_varint(n: int) -> bytes:
    if n < 0xfd:
        return bytes([n])
    if n <= 0xffff:
        return b"\xfd" + n.to_bytes(2, "little")
    if n <= 0xffffffff:
        return b"\xfe" + n.to_bytes(4, "little")
    return b"\xff" + n.to_bytes(8, "little")


def script_num(n: int) -> bytes:
    """Minimal CScript number encoding (non-negative n), used for BIP34 height."""
    if n == 0:
        return b""
    out = bytearray()
    while n:
        out.append(n & 0xff)
        n >>= 8
    if out[-1] & 0x80:      # keep it unambiguously positive
        out.append(0x00)
    return bytes(out)


def bip34_height_push(height: int) -> bytes:
    """Push of the block height as required at the start of the coinbase scriptSig."""
    enc = script_num(height)
    return bytes([len(enc)]) + enc


MAX_TAG_LEN = 64            # characters (= bytes: printable ASCII only)
MAX_SCRIPT_SIG = 100        # consensus: a coinbase scriptSig is 2..100 bytes


def coinbase_tag_error(tag):
    """None if `tag` can be the coinbase tag (the text a found block carries
    after the height and extranonce), else a short reason it cannot. Printable
    ASCII only, so what the Settings page shows is exactly what a block
    explorer shows, and the byte count is the character count."""
    if not isinstance(tag, str):
        return "must be text"
    if len(tag) > MAX_TAG_LEN:
        return f"must be at most {MAX_TAG_LEN} characters"
    if any(not " " <= c <= "~" for c in tag):
        return ("may only use plain characters: letters, digits, spaces and "
                "punctuation (no emoji or accented letters)")
    return None


def build_coinbase_parts(height, coinbase_value, payout_script,
                         extranonce1_size, extranonce2_size,
                         tag=b"", tx_version=2, witness_commitment=None):
    """Return (coinb1_hex, coinb2_hex) for Stratum mining.notify.

    The caller inserts extranonce1 (server) + extranonce2 (miner) between them.
    """
    if isinstance(payout_script, str):
        payout_script = bytes.fromhex(payout_script)
    if isinstance(tag, str):
        tag = tag.encode()
    if isinstance(witness_commitment, str):
        witness_commitment = bytes.fromhex(witness_commitment)

    height_push = bip34_height_push(height)
    script_sig_len = len(height_push) + extranonce1_size + extranonce2_size + len(tag)
    if script_sig_len > MAX_SCRIPT_SIG:
        # The node would reject every block built from this job.
        raise ValueError(f"coinbase scriptSig would be {script_sig_len} bytes "
                         f"(max {MAX_SCRIPT_SIG}): shorten the coinbase tag")

    # coinb1: up to (and including) the BIP34 height push; extranonce follows.
    coinb1 = (
        tx_version.to_bytes(4, "little")
        + encode_varint(1)                       # input count
        + b"\x00" * 32                           # prevout hash (null)
        + b"\xff\xff\xff\xff"                    # prevout index
        + encode_varint(script_sig_len)          # scriptSig length
        + height_push
    )

    # coinb2: tag (rest of scriptSig) + sequence + outputs + locktime.
    output = (
        int(coinbase_value).to_bytes(8, "little")
        + encode_varint(len(payout_script))
        + payout_script
    )
    outputs = [output]
    if witness_commitment:
        outputs.append(b"\x00" * 8                     # value 0
                       + encode_varint(len(witness_commitment))
                       + witness_commitment)
    coinb2 = (
        tag
        + b"\xff\xff\xff\xff"                    # sequence
        + encode_varint(len(outputs))            # payout (+ witness commitment)
        + b"".join(outputs)
        + b"\x00\x00\x00\x00"                    # locktime
    )
    return coinb1.hex(), coinb2.hex()


def assemble(coinb1_hex, extranonce1_hex, extranonce2_hex, coinb2_hex) -> bytes:
    return bytes.fromhex(coinb1_hex + extranonce1_hex + extranonce2_hex + coinb2_hex)


WITNESS_RESERVED_VALUE = b"\x00" * 32


def with_witness(coinbase_raw: bytes) -> bytes:
    """The witness serialization of a non-witness coinbase (BIP144): marker +
    flag after the version, and before the locktime the one input's witness
    stack = [32-byte witness reserved value]. The txid (sha256d of the input)
    is unchanged; this form is only for the block sent to submitblock."""
    return (coinbase_raw[:4] + b"\x00\x01" + coinbase_raw[4:-4]
            + b"\x01\x20" + WITNESS_RESERVED_VALUE + coinbase_raw[-4:])


# --------------------------------------------------------------------------- #
# Self-test: build, then parse back and verify every field
# --------------------------------------------------------------------------- #
def _read_varint(b, i):
    n = b[i]; i += 1
    if n < 0xfd:
        return n, i
    if n == 0xfd:
        return int.from_bytes(b[i:i+2], "little"), i+2
    if n == 0xfe:
        return int.from_bytes(b[i:i+4], "little"), i+4
    return int.from_bytes(b[i:i+8], "little"), i+8


def _selftest():
    import hashlib
    ok = fail = 0

    def check(label, cond):
        nonlocal ok, fail
        if cond:
            ok += 1
        else:
            fail += 1
            print(f"FAIL  {label}")

    # BIP34 height encodings
    check("height 1 -> 0101", bip34_height_push(1).hex() == "0101")
    check("height 800000 -> 0300350c", bip34_height_push(800000).hex() == "0300350c")
    check("height 129 -> 028100 (positive sign pad)", bip34_height_push(129).hex() == "028100")

    # Build a realistic coinbase
    height = 800123
    value = 3_12500000            # 3.125 BCH subsidy example (satoshis)
    payout = bytes.fromhex("76a914cb481232299cd5743151ac4b2d63ae198e7bb0a988ac")
    en1 = "deadbeef"             # 4 bytes
    en2 = "01020304"            # 4 bytes
    tag = b"/AxePool/"
    c1, c2 = build_coinbase_parts(height, value, payout, 4, 4, tag=tag)
    raw = assemble(c1, en1, en2, c2)

    # Parse it back
    i = 0
    ver = int.from_bytes(raw[i:i+4], "little"); i += 4
    vin, i = _read_varint(raw, i)
    prevout = raw[i:i+32]; i += 32
    index = raw[i:i+4]; i += 4
    ss_len, i = _read_varint(raw, i)
    script_sig = raw[i:i+ss_len]; i += ss_len
    seq = raw[i:i+4]; i += 4
    vout, i = _read_varint(raw, i)
    out_value = int.from_bytes(raw[i:i+8], "little"); i += 8
    spk_len, i = _read_varint(raw, i)
    spk = raw[i:i+spk_len]; i += spk_len
    locktime = raw[i:i+4]; i += 4

    check("tx version == 2", ver == 2)
    check("single input", vin == 1)
    check("null prevout", prevout == b"\x00" * 32)
    check("prevout index ffffffff", index == b"\xff\xff\xff\xff")
    check("scriptSig = height+en1+en2+tag",
          script_sig == bip34_height_push(height) + bytes.fromhex(en1)
          + bytes.fromhex(en2) + tag)
    check("scriptSig starts with BIP34 height",
          script_sig[:len(bip34_height_push(height))] == bip34_height_push(height))
    check("sequence ffffffff", seq == b"\xff\xff\xff\xff")
    check("single output", vout == 1)
    check("output value == coinbasevalue", out_value == value)
    check("output script == payout", spk == payout)
    check("locktime 0", locktime == b"\x00\x00\x00\x00")
    check("no trailing bytes (fully consumed)", i == len(raw))
    check("extranonce region is between the halves",
          raw[len(bytes.fromhex(c1)):len(bytes.fromhex(c1))+8].hex() == en1 + en2)

    # A txid can be computed (double-SHA256, shown big-endian)
    txid = hashlib.sha256(hashlib.sha256(raw).digest()).digest()[::-1].hex()
    print(f"\nsample coinbase txid: {txid}")
    print(f"coinb1 ({len(bytes.fromhex(c1))} B): {c1}")
    print(f"coinb2 ({len(bytes.fromhex(c2))} B): {c2}")

    print(f"\n{ok} passed, {fail} failed")
    return fail == 0


if __name__ == "__main__":
    import sys
    sys.exit(0 if _selftest() else 1)
