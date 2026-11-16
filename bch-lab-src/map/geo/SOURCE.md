# geo.bin

Built by `tools/gengeo.py` from **DB-IP IP to City Lite**, licensed CC BY 4.0
(<https://db-ip.com>). The dashboard footer carries the attribution link the
licence asks for; keep it.

Source: `dbip-city-ipv4.mmdb` and `dbip-city-ipv6.mmdb` from the `latest`
release of <https://github.com/sapics/ip-location-db>, which republishes that
data as `.mmdb`. Current table: database build 2026-10-02 (IPv4 and IPv6), 3,531 regions,
2,088,036 IPv4 and 2,174,150 IPv6 ranges, 34,331,474 bytes.

**Not from npm.** The same project publishes these databases as npm packages
too, and warns on them that the data there contains errors and that npm updates
have stopped. The releases above are the corrected ones. `asn/asn.bin` comes
from the same place for the same reason.

## Refresh

```sh
pip install maxminddb
base=https://github.com/sapics/ip-location-db/releases/download/latest
curl -sLO $base/dbip-city-ipv4.mmdb -O $base/dbip-city-ipv6.mmdb
python3 tools/gengeo.py dbip-city-ipv4.mmdb dbip-city-ipv6.mmdb geo/geo.bin
go test ./geo/
```

This takes a few minutes. It also rewrites `geo/testdata/vectors.json`, the
sample addresses `go test ./geo/` checks against.

IPv6 is keyed on the upper 64 bits, so where two regions live inside one /64
only the later one survives the build: 2,491 keys on this database. Those are
left out of the test vectors rather than covered by a tolerance - a test that
allows a few wrong answers stops noticing when the number grows.

Region names in the source are not consistent ("Berlin" / "State of Berlin",
"ACT" / "Australian Capital Territory"); `region_key` in the generator folds
them, and `TestRegionNamesFolded` checks Germany still has 16 and Australia 8.

# web/world.json

Built by `tools/genmap` from Natural Earth (public domain),
`ne_110m_admin_0_countries.geojson` for outlines and
`ne_50m_admin_0_countries.geojson` for country marker positions, both from
<https://github.com/nvkelso/natural-earth-vector/tree/master/geojson>.

```sh
go run ./tools/genmap -shapes ne_110m_admin_0_countries.geojson \
    -labels ne_50m_admin_0_countries.geojson -o web/world.json
```
