#!/usr/bin/env python3
"""Build asn/asn.bin - the network-operator table Peer Map embeds - from
ip-location-db's DB-IP ASN database (CC BY 4.0, see geo/SOURCE.md).

    pip install maxminddb
    base=https://github.com/sapics/ip-location-db/releases/download/latest
    curl -sLO $base/dbip-asn-ipv4.mmdb -O $base/dbip-asn-ipv6.mmdb
    python3 tools/genasn.py dbip-asn-ipv4.mmdb dbip-asn-ipv6.mmdb asn/asn.bin

The map answers "where", this answers "whose". They are different questions and
the second is often the more useful one for a node: three peers can sit in three
different countries and still be three machines in one data centre, which is no
diversity at all - measured on one real node, three of eight manual peers were
in three different /16 networks, in the same country, all at the same operator.
Only the AS number showed it.

What the app needs is the number and a name, so the database is reduced to
exactly that. Adjacent ranges belonging to the same AS are merged, and the gaps
between assigned ranges get an explicit "unknown" entry so a lookup cannot fall
through into the next range and answer with somebody else's network.

Layout (big-endian), the same shape as geo.bin so the two readers stay familiar:
    "PMASN1"
    nAS uint32, then per AS: number uint32, name length uint8, name UTF-8
    n4 uint32, then n4 x uint32 range start, then n4 x uint32 AS index
    n6 uint32, then n6 x uint64 range start (upper 64 bits), then n6 x uint32 AS index
AS index 0 is "unknown" (number 0, empty name) and marks the gaps. IPv6 is keyed
on the upper 64 bits, exactly as in geo.bin; where ranges collapse onto the same
64-bit start, the later one wins.

It also writes asn/testdata/vectors.json: addresses sampled from the databases
with the AS a direct lookup in the .mmdb gives for them, for asn_test.go.
"""
import ipaddress
import json
import random
import struct
import sys

import maxminddb

UNKNOWN = 0  # index 0 in the AS table


def read(path, v6, names):
    """Sorted (start, as_number) with gaps marked, from one .mmdb."""
    db = maxminddb.open_database(path)
    rows = []
    for net, rec in db:
        if not rec:
            continue
        num = rec.get("autonomous_system_number")
        if num is None:
            continue
        name = (rec.get("autonomous_system_organization") or "").strip()
        # The same AS appears with slightly different names across ranges.
        # Keep the shortest non-empty one: it is the plainest.
        if name and (num not in names or len(name) < len(names[num])):
            names[num] = name
        net = ipaddress.ip_network(net)
        start, end = int(net.network_address), int(net.broadcast_address)
        if v6:
            start >>= 64
            end >>= 64
        rows.append((start, end, num))
    db.close()
    rows.sort()

    out = []
    prev_end = -1
    for start, end, num in rows:
        if start > prev_end + 1 and prev_end >= 0:
            out.append((prev_end + 1, UNKNOWN))  # a gap: nobody announces this
        if out and out[-1][1] == num:
            prev_end = max(prev_end, end)
            continue
        out.append((start, num))
        prev_end = max(prev_end, end)
    return out


def main(v4_path, v6_path, out_path):
    names = {}
    v4 = read(v4_path, False, names)
    v6 = read(v6_path, True, names)

    # AS table: index 0 is "unknown", the rest sorted by number so the file is
    # reproducible from the same input.
    numbers = sorted(names)
    index = {num: i + 1 for i, num in enumerate(numbers)}
    index[0] = UNKNOWN

    be = struct.Struct(">I")
    parts = [b"PMASN1", be.pack(len(numbers) + 1)]
    parts.append(be.pack(0) + bytes([0]))  # the unknown entry
    for num in numbers:
        name = names[num].encode()[:255]
        parts.append(be.pack(num) + bytes([len(name)]) + name)

    def block(rows, start_fmt):
        s = struct.Struct(start_fmt)
        parts.append(be.pack(len(rows)))
        parts.append(b"".join(s.pack(start) for start, _ in rows))
        parts.append(b"".join(be.pack(index.get(num, UNKNOWN)) for _, num in rows))

    block(v4, ">I")
    block(v6, ">Q")

    blob = b"".join(parts)
    with open(out_path, "wb") as fh:
        fh.write(blob)
    print(f"{out_path}: {len(blob):,} bytes - {len(numbers):,} networks, "
          f"{len(v4):,} IPv4 ranges, {len(v6):,} IPv6 ranges")
    return v4_path, v6_path


def contested(path):
    """Upper-64 keys that more than one AS shares.

    The table keys IPv6 on the upper 64 bits, so where two networks live inside
    one /64 only one of them survives, and there is no address in those that
    the app could answer correctly. They are left out of the test vectors
    rather than papered over with a tolerance: a test that allows a few wrong
    answers stops noticing when the number grows.
    """
    db = maxminddb.open_database(path)
    seen = {}
    bad = set()
    for net, rec in db:
        if not rec:
            continue
        num = rec.get("autonomous_system_number")
        if num is None:
            continue
        key = int(ipaddress.ip_network(net).network_address) >> 64
        if key in seen and seen[key] != num:
            bad.add(key)
        seen[key] = num
    db.close()
    return bad


def vectors(v4_path, v6_path, out_path, n=300):
    """Sample addresses and record what a direct .mmdb lookup says about them."""
    rng = random.Random(20260917)
    skip = contested(v6_path)
    out = []
    dropped = 0
    for path, v6 in ((v4_path, False), (v6_path, True)):
        db = maxminddb.open_database(path)
        nets = [net for net, rec in db if rec and rec.get("autonomous_system_number")]
        if v6:
            before = len(nets)
            nets = [x for x in nets
                    if (int(ipaddress.ip_network(x).network_address) >> 64) not in skip]
            dropped = before - len(nets)
        for net in rng.sample(nets, min(n, len(nets))):
            net = ipaddress.ip_network(net)
            addr = ipaddress.ip_address(net.network_address + min(1, net.num_addresses - 1))
            rec = db.get(str(addr))
            if not rec:
                continue
            out.append({
                "addr": str(addr),
                "asn": rec["autonomous_system_number"],
                "name": (rec.get("autonomous_system_organization") or "").strip(),
            })
        db.close()
    with open(out_path, "w") as fh:
        json.dump(out, fh, indent=1)
    print(f"{out_path}: {len(out)} vectors "
          f"({len(skip)} contested /64 keys excluded, {dropped} ranges)")


if __name__ == "__main__":
    if len(sys.argv) != 4:
        sys.exit(__doc__)
    a, b, c = sys.argv[1:4]
    main(a, b, c)
    vectors(a, b, c.rsplit("/", 1)[0] + "/testdata/vectors.json")
