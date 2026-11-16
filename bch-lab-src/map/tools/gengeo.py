#!/usr/bin/env python3
"""Build geo/geo.bin - the region table Peer Map embeds - from DB-IP's
"IP to City Lite" database (CC BY 4.0, https://db-ip.com).

    pip install maxminddb
    base=https://github.com/sapics/ip-location-db/releases/download/latest
    curl -sLO $base/dbip-city-ipv4.mmdb -O $base/dbip-city-ipv6.mmdb
    python3 tools/gengeo.py dbip-city-ipv4.mmdb dbip-city-ipv6.mmdb geo/geo.bin

The app only needs "which region, of which country, and roughly where", so
the city data is reduced to one entry per region (state / province, the
database's `state1`) with the average position of that region's address
ranges, plus a sorted list of range starts pointing at a region. Adjacent
ranges in the same region are merged, which is what keeps it small.

`state1` is not consistent: the same region appears as "Berlin" and "State of
Berlin", "ACT" and "Australian Capital Territory", with or without
"District", "Province", "Municipality of" and so on. Names are folded to a key
(`region_key`) before grouping; the displayed name is the plainest variant
seen. Distance is deliberately not used to merge: Berlin's and Brandenburg's
address ranges average out less than 10 km apart.

Layout (big-endian):
    "PMGEO2"
    nRegions uint16, then per region: cc 2 bytes ASCII ("--" = no data),
        lat int16 (degrees x100), lon int16 (degrees x100),
        name length uint8, name UTF-8 ("" = country only, no region)
    n4 uint32, then n4 x uint32 range start, then n4 x uint16 region index
    n6 uint32, then n6 x uint64 range start (upper 64 bits), then n6 x uint16 region index
Region 0 is "no data". IPv6 is keyed on the upper 64 bits; where ranges
collapse onto the same 64-bit start, the later one wins.

It also writes geo/testdata/vectors.json: addresses sampled from the
databases with the region that a direct lookup in the .mmdb gives for them,
for geo_test.go.
"""
import collections, ipaddress, json, random, re, struct, sys, unicodedata

import maxminddb

PREFIX = ["free and hanseatic city of ", "free hanseatic city of ", "urban municipality of ",
          "municipality of ", "collectivity of ", "state of ", "city state ", "obcina ",
          "province of ", "region of "]
SUFFIX = [" city municipality", " municipality", " autonomous district", " governorate",
          " department", " division", " district", " region", " province", " county",
          " parish", " sheng", " oblast", " state", " city", " prefecture"]
ALIAS = {("AU", "act"): "australian capital territory"}


def fold(s):
    s = unicodedata.normalize("NFKD", s).encode("ascii", "ignore").decode().lower()
    s = re.sub(r"[‐-—_]", "-", s)
    s = re.sub(r"[^a-z0-9 -]", "", s)
    return re.sub(r"\s+", " ", s).strip()


def region_key(cc, name):
    k = fold(name)
    for p in PREFIX:
        if k.startswith(p):
            k = k[len(p):]
    changed = True
    while changed:
        changed = False
        for suf in SUFFIX:
            if k.endswith(suf) and len(k) > len(suf) + 2:
                k = k[: -len(suf)]
                changed = True
    k = k.replace("-", " ")
    return ALIAS.get((cc, k), k)


def main(v4path, v6path, outpath):
    groups = {}            # (cc, key) -> index
    info = [None]          # index -> [cc, lat_sum, lon_sum, n, Counter(names)]
    info[0] = ["--", 0.0, 0.0, 0, collections.Counter()]
    tables = {}
    samples = []
    rnd = random.Random(7)
    # IPv6 is keyed on the upper 64 bits, so two regions inside one /64 cannot
    # both survive - the later one wins. Those keys are collected here and left
    # out of the test vectors: a test that allows a few wrong answers stops
    # noticing when the number grows.
    contested = set()

    for fam, path in (("v4", v4path), ("v6", v6path)):
        reader = maxminddb.open_database(path)
        starts, idxs = [], []
        last_end = None
        prev_e, prev_gi = -1, None
        for net, rec in reader:
            cc = rec.get("country_code") or "--"
            name = (rec.get("state1") or "").strip()
            gk = (cc, region_key(cc, name) if name else "")
            gi = groups.get(gk)
            if gi is None:
                gi = groups[gk] = len(info)
                info.append([cc, 0.0, 0.0, 0, collections.Counter()])
            g = info[gi]
            g[1] += rec["latitude"]; g[2] += rec["longitude"]; g[3] += 1
            if name:
                g[4][name] += 1
            s, e = int(net.network_address), int(net.broadcast_address)
            if fam == "v6":
                s >>= 64
                e >>= 64
                # This range shares a /64 with the one before it and says
                # something else: one of the two answers is about to be lost.
                if prev_gi is not None and s <= prev_e and gi != prev_gi:
                    contested.add(s)
                prev_e, prev_gi = max(prev_e, e), gi
            if last_end is not None and s > last_end + 1:
                starts.append(last_end + 1); idxs.append(0)          # hole: no data
            if starts and idxs[-1] == gi:
                pass                                                 # same region continues
            elif starts and starts[-1] == s:
                idxs[-1] = gi                                        # v6 collapse: the later one wins
            else:
                starts.append(s); idxs.append(gi)
            last_end = e if last_end is None else max(last_end, e)
            if rnd.random() < 300 / 3_000_000:
                addr = ipaddress.ip_address(int(net.network_address) + (int(net.num_addresses) // 2 if fam == "v4" else 0))
                samples.append((str(addr), fam, cc, name))
        top = 2**32 - 1 if fam == "v4" else 2**64 - 1
        if last_end is not None and last_end < top:
            starts.append(last_end + 1); idxs.append(0)
        tables[fam] = (starts, idxs)
        print(f"{fam}: {len(starts)} ranges", file=sys.stderr)

    if len(info) > 65535:
        sys.exit(f"{len(info)} regions do not fit a uint16 index")

    def display(counter, cc):
        if not counter:
            return ""
        names = counter.most_common()
        # Prefer the plain variant ("Berlin" over "State of Berlin").
        for n, _ in names:
            if fold(n).replace("-", " ") == region_key(cc, n):
                return n
        return names[0][0]

    out = bytearray(b"PMGEO2")
    out += struct.pack(">H", len(info))
    names_by_index = []
    for i, (cc, la, lo, n, counter) in enumerate(info):
        name = display(counter, cc)
        names_by_index.append(name)
        lat = round(la / n * 100) if n else 0
        lon = round(lo / n * 100) if n else 0
        nb = name.encode("utf-8")[:255]
        out += cc.encode("ascii") + struct.pack(">hhB", lat, lon, len(nb)) + nb
    for fam, fmt in (("v4", "I"), ("v6", "Q")):
        starts, idxs = tables[fam]
        out += struct.pack(">I", len(starts))
        out += struct.pack(f">{len(starts)}{fmt}", *starts)
        out += struct.pack(f">{len(idxs)}H", *idxs)
    open(outpath, "wb").write(out)
    print(f"regions={len(info)} -> {outpath} ({len(out)} bytes)", file=sys.stderr)

    vectors = []
    dropped = 0
    for addr, fam, cc, name in samples:
        if fam == "v6" and (int(ipaddress.ip_address(addr)) >> 64) in contested:
            dropped += 1
            continue
        want = names_by_index[groups[(cc, region_key(cc, name) if name else "")]]
        vectors.append([addr, cc, want])
    json.dump(vectors, open("geo/testdata/vectors.json", "w"), ensure_ascii=False)
    print(f"{len(vectors)} test vectors "
          f"({len(contested):,} contested /64 keys, {dropped} samples dropped)", file=sys.stderr)


if __name__ == "__main__":
    if len(sys.argv) != 4:
        sys.exit(__doc__)
    main(*sys.argv[1:])
