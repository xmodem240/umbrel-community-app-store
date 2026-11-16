# asn.bin

Built by `tools/genasn.py` from **DB-IP's IP to ASN Lite** database, licensed
CC BY 4.0 (<https://db-ip.com>). The dashboard footer carries the DB-IP
attribution link the licence asks for; it covers this table and `geo.bin`.

Source: `dbip-asn-ipv4.mmdb` and `dbip-asn-ipv6.mmdb` from the `latest` release
of <https://github.com/sapics/ip-location-db>, which republishes that data as
`.mmdb`. Current table: database build 2026-11-05, 83,024 networks, 487,505
IPv4 and 130,543 IPv6 ranges, 7,915,025 bytes.

**Not from npm.** The same project publishes these databases as npm packages
too, and warns on them that the data there contains errors and that npm updates
have stopped. The releases above are the corrected ones and are rebuilt daily.
`geo.bin` comes from the same place for the same reason.

The database carries a number, a name and a range. Only those three are kept,
adjacent ranges of the same network are merged, and the gaps between assigned
ranges get an explicit "unknown" entry so a lookup cannot fall through into the
next range and answer with somebody else's network.

## Refresh

```sh
pip install maxminddb
base=https://github.com/sapics/ip-location-db/releases/download/latest
curl -sLO $base/dbip-asn-ipv4.mmdb -O $base/dbip-asn-ipv6.mmdb
python3 tools/genasn.py dbip-asn-ipv4.mmdb dbip-asn-ipv6.mmdb asn/asn.bin
go test ./asn/
```

This also rewrites `asn/testdata/vectors.json`, the 600 sample addresses
`go test ./asn/` checks the rebuilt table against, each carrying the answer the
source database itself gives.

## What the table cannot answer

IPv6 is keyed on the upper 64 bits, the same as `geo.bin`. Where two networks
share one `/64`, only one of them survives the build: on the September 2026
database that is 70 keys, and no address in them can be answered correctly.
Those keys are left out of the test vectors rather than covered by a tolerance
— a test that allows a few wrong answers stops noticing when the number grows.

An address nobody announces returns `ok=false`, and so does every address that
is not a public IP: Tor, I2P, CJDNS, and the `10.21.0.1` that docker-proxy puts
in front of inbound IPv6 on umbrelOS. That is a gap in the routing table or a
network without one, not a failure.
