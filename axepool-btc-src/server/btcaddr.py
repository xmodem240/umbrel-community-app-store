#!/usr/bin/env python3
"""
AxePool BTC - Bitcoin payout address -> scriptPubKey.

Accepted (the coinbase output pays exactly this script):
  * legacy Base58Check  1... (P2PKH)  3... (P2SH)          [testnet/regtest: m/n, 2]
  * SegWit v0, bech32   bc1q... 20-byte (P2WPKH) / 32-byte (P2WSH)   (BIP173)
  * Taproot v1, bech32m bc1p... 32-byte (P2TR)                       (BIP350)

Refused on purpose: witness versions 2..16 and v1 programs that are not 32
bytes. Those decode as valid addresses, but today their outputs can be spent
by anyone, so a block reward paid to one could be taken by a stranger.

`network` is the node's chain name as getblockchaininfo reports it
("main", "test", "testnet4", "signet", "regtest"); an address for another
network is refused. Standard library only.

Self-test against Bitcoin Core's own vectors (src/test/data/key_io_*.json,
copied into fixtures/):   python3 btcaddr.py
"""
import hashlib

B58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
BECH32_CHARSET = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
BECH32_CONST, BECH32M_CONST = 1, 0x2bc830a3

# chain -> (bech32 hrp, P2PKH version byte, P2SH version byte)
NETWORKS = {
    "main": ("bc", 0x00, 0x05),
    "test": ("tb", 0x6f, 0xc4),
    "testnet4": ("tb", 0x6f, 0xc4),
    "signet": ("tb", 0x6f, 0xc4),
    "regtest": ("bcrt", 0x6f, 0xc4),
}
# Umbrel's APP_BITCOIN_NETWORK values -> getblockchaininfo chain names.
UMBREL_NETWORK = {"mainnet": "main", "testnet": "test", "testnet3": "test",
                  "testnet4": "testnet4", "signet": "signet", "regtest": "regtest"}


class AddressError(ValueError):
    pass


def chain_name(network):
    """Normalize 'mainnet'/'main'/... to a getblockchaininfo chain name."""
    n = (network or "main").strip().lower()
    n = UMBREL_NETWORK.get(n, n)
    if n not in NETWORKS:
        raise AddressError(f"unknown Bitcoin network {network!r}")
    return n


# --- Base58Check ------------------------------------------------------------ #
def _b58decode_check(s):
    n = 0
    for c in s:
        i = B58.find(c)
        if i < 0:
            raise AddressError(f"invalid Base58 character {c!r}")
        n = n * 58 + i
    zeros = len(s) - len(s.lstrip("1"))
    raw = b"\x00" * zeros + (n.to_bytes((n.bit_length() + 7) // 8, "big") if n else b"")
    if len(raw) < 5:
        raise AddressError("too short")
    payload, check = raw[:-4], raw[-4:]
    if hashlib.sha256(hashlib.sha256(payload).digest()).digest()[:4] != check:
        raise AddressError("bad checksum (mistyped address?)")
    return payload


# --- bech32 / bech32m (BIP173, BIP350) -------------------------------------- #
def _polymod(values):
    gen = (0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3)
    chk = 1
    for v in values:
        top = chk >> 25
        chk = (chk & 0x1ffffff) << 5 ^ v
        for i in range(5):
            chk ^= gen[i] if (top >> i) & 1 else 0
    return chk


def _hrp_expand(hrp):
    return [ord(x) >> 5 for x in hrp] + [0] + [ord(x) & 31 for x in hrp]


def _bech32_decode(addr):
    if any(ord(c) < 33 or ord(c) > 126 for c in addr):
        raise AddressError("invalid character")
    if addr.lower() != addr and addr.upper() != addr:
        raise AddressError("mixed upper and lower case")
    addr = addr.lower()
    pos = addr.rfind("1")
    if pos < 1 or pos + 7 > len(addr) or len(addr) > 90:
        raise AddressError("not a valid bech32 address")
    hrp, data = addr[:pos], []
    for c in addr[pos + 1:]:
        d = BECH32_CHARSET.find(c)
        if d < 0:
            raise AddressError(f"invalid bech32 character {c!r}")
        data.append(d)
    const = _polymod(_hrp_expand(hrp) + data)
    if const == BECH32_CONST:
        enc = "bech32"
    elif const == BECH32M_CONST:
        enc = "bech32m"
    else:
        raise AddressError("bad checksum (mistyped address?)")
    return hrp, data[:-6], enc


def _convertbits(data, frombits, tobits):
    acc = bits = 0
    out, maxv = [], (1 << tobits) - 1
    for v in data:
        if v >> frombits:
            raise AddressError("invalid data")
        acc = (acc << frombits) | v
        bits += frombits
        while bits >= tobits:
            bits -= tobits
            out.append((acc >> bits) & maxv)
    if bits >= frombits or ((acc << (tobits - bits)) & maxv):
        raise AddressError("invalid padding")
    return bytes(out)


def _segwit_decode(addr, hrp):
    got_hrp, data, enc = _bech32_decode(addr)
    if got_hrp != hrp:
        raise AddressError(f"address is for another network (prefix {got_hrp}1, "
                           f"this node expects {hrp}1)")
    if not data:
        raise AddressError("empty witness data")
    ver = data[0]
    if ver > 16:
        raise AddressError("invalid witness version")
    prog = _convertbits(data[1:], 5, 8)
    if not 2 <= len(prog) <= 40:
        raise AddressError("invalid witness program length")
    if ver == 0 and len(prog) not in (20, 32):
        raise AddressError("invalid v0 witness program length")
    if (ver == 0) != (enc == "bech32"):
        raise AddressError("wrong checksum type for this witness version "
                           "(v0 uses bech32, v1+ bech32m)")
    return ver, prog


def decode(address, network="main"):
    """Return {"type": ..., "script": bytes} for a payout address on `network`,
    or raise AddressError with a human-readable reason."""
    if not isinstance(address, str):
        raise AddressError("address must be text")
    address = address.strip()
    if not address:
        raise AddressError("empty address")
    chain = chain_name(network)
    hrp, p2pkh, p2sh = NETWORKS[chain]

    if address.lower().startswith(hrp + "1"):
        ver, prog = _segwit_decode(address, hrp)
        if ver == 0:
            kind = "p2wpkh" if len(prog) == 20 else "p2wsh"
        elif ver == 1 and len(prog) == 32:
            kind = "p2tr"
        else:
            raise AddressError(f"witness v{ver} ({len(prog)}-byte) outputs can be spent "
                               "by anyone today; refusing to pay a block reward to one")
        op = 0 if ver == 0 else 0x50 + ver
        return {"type": kind, "script": bytes([op, len(prog)]) + prog}

    if any(address.lower().startswith(h + "1") for h, _, _ in NETWORKS.values()):
        raise AddressError(f"address is for another network (this node expects {hrp}1...)")

    payload = _b58decode_check(address)
    if len(payload) != 21:
        raise AddressError("legacy address has the wrong length")
    ver, h160 = payload[0], payload[1:]
    if ver == p2pkh:
        return {"type": "p2pkh", "script": b"\x76\xa9\x14" + h160 + b"\x88\xac"}
    if ver == p2sh:
        return {"type": "p2sh", "script": b"\xa9\x14" + h160 + b"\x87"}
    raise AddressError("legacy address is for another network or not a payment address")


def to_script(address, network="main"):
    return decode(address, network)["script"]


# --------------------------------------------------------------------------- #
def _selftest():
    import json
    import os
    here = os.path.join(os.path.dirname(os.path.abspath(__file__)), "fixtures")
    ok = fail = 0

    def check(label, cond):
        nonlocal ok, fail
        if cond:
            ok += 1
        else:
            fail += 1
            print(f"FAIL  {label}")

    with open(os.path.join(here, "bitcoin_core_key_io_valid.json")) as f:
        valid = [e for e in json.load(f) if not e[2].get("isPrivkey")]
    with open(os.path.join(here, "bitcoin_core_key_io_invalid.json")) as f:
        invalid = [e[0] for e in json.load(f)]

    payable = refused_unsafe = 0
    for addr, spk, meta in valid:
        chain = meta["chain"]
        spk = bytes.fromhex(spk)
        # Witness v2+ / non-32-byte v1: Bitcoin Core calls them valid; we refuse.
        is_witness = (spk[0] == 0 or 0x51 <= spk[0] <= 0x60) and len(spk) == 2 + spk[1]
        unsafe = is_witness and spk[0] != 0 and not (spk[0] == 0x51 and spk[1] == 32)
        try:
            got = to_script(addr, chain)
            check(f"{addr} -> Core's scriptPubKey", not unsafe and got == spk)
            payable += 1
        except AddressError as e:
            check(f"{addr} refused only because it is anyone-can-spend ({e})", unsafe)
            refused_unsafe += 1
        # ...and refused where that network's prefix / version bytes differ.
        # (Legacy m/n/2 addresses are shared by testnet, signet and regtest in
        # Bitcoin Core too; bech32 tb1 vs bcrt1 is not.)
        for other in ("main", "testnet4", "regtest"):
            same = (NETWORKS[other][0] == NETWORKS[chain][0] if is_witness
                    else NETWORKS[other][1:] == NETWORKS[chain][1:])
            if same:
                continue
            try:
                to_script(addr, other)
                check(f"{addr} ({chain}) refused on {other}", False)
            except AddressError:
                check(f"{addr} ({chain}) refused on {other}", True)
    check("some vectors are payable", payable >= 30)
    check("some unsafe vectors exercised", refused_unsafe >= 1)

    for addr in invalid:
        for chain in ("main", "testnet4", "signet", "regtest"):
            try:
                to_script(addr, chain)
                check(f"invalid {addr!r} refused on {chain}", False)
            except AddressError:
                check(f"invalid {addr!r} refused on {chain}", True)

    # Umbrel network names
    check("mainnet -> main", chain_name("mainnet") == "main")
    check("regtest -> regtest", chain_name("regtest") == "regtest")
    print(f"{ok} passed, {fail} failed  ({payable} payable vectors, "
          f"{refused_unsafe} anyone-can-spend refused, {len(invalid)} invalid)")
    return fail == 0


if __name__ == "__main__":
    import sys
    sys.exit(0 if _selftest() else 1)
