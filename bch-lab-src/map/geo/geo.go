// Package geo maps an IP address to a country and region using the table in
// geo.bin, built by tools/gengeo.py from DB-IP's IP to City Lite data
// (CC BY 4.0 - https://db-ip.com).
//
// The table is embedded in the binary and searched in place: nothing is
// decoded into the heap except the small region list, and the kernel only
// pages in the few kilobytes a binary search actually touches.
package geo

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"net/netip"
	"sort"
)

//go:embed geo.bin
var raw []byte

// Location is what a lookup returns. Region is "" when the data only knows
// the country; Lat/Lon are then zero and meaningless.
type Location struct {
	ID      uint16  // index into the region table, stable for one build of geo.bin
	Country string  // ISO 3166-1 alpha-2
	Region  string  // state / province as DB-IP names it
	Lat     float64 // average position of the region's address ranges
	Lon     float64
}

type table struct {
	regions []Location
	n4      int
	s4, c4  []byte
	n6      int
	s6, c6  []byte
}

var tbl table

func init() {
	t, err := parse(raw)
	if err != nil {
		panic("geo.bin: " + err.Error())
	}
	tbl = t
}

func parse(b []byte) (table, error) {
	var t table
	be := binary.BigEndian
	if len(b) < 8 || string(b[:6]) != "PMGEO2" {
		return t, errors.New("bad magic")
	}
	p := 6
	need := func(n int) error {
		if n < 0 || p+n > len(b) {
			return errors.New("truncated")
		}
		return nil
	}
	nr := int(be.Uint16(b[p:]))
	p += 2
	t.regions = make([]Location, nr)
	for i := 0; i < nr; i++ {
		if err := need(7); err != nil {
			return t, err
		}
		cc := string(b[p : p+2])
		lat := int16(be.Uint16(b[p+2:]))
		lon := int16(be.Uint16(b[p+4:]))
		nl := int(b[p+6])
		p += 7
		if err := need(nl); err != nil {
			return t, err
		}
		name := string(b[p : p+nl])
		p += nl
		t.regions[i] = Location{ID: uint16(i), Country: cc, Region: name, Lat: float64(lat) / 100, Lon: float64(lon) / 100}
	}
	if err := need(4); err != nil {
		return t, err
	}
	t.n4 = int(be.Uint32(b[p:]))
	p += 4
	if err := need(t.n4 * 6); err != nil {
		return t, err
	}
	t.s4 = b[p : p+t.n4*4]
	p += t.n4 * 4
	t.c4 = b[p : p+t.n4*2]
	p += t.n4 * 2
	if err := need(4); err != nil {
		return t, err
	}
	t.n6 = int(be.Uint32(b[p:]))
	p += 4
	if err := need(t.n6 * 10); err != nil {
		return t, err
	}
	t.s6 = b[p : p+t.n6*8]
	p += t.n6 * 8
	t.c6 = b[p : p+t.n6*2]
	p += t.n6 * 2
	if p != len(b) {
		return t, errors.New("trailing bytes")
	}
	return t, nil
}

// Lookup returns the location of a public address; ok is false when the
// table has no data for it.
func Lookup(a netip.Addr) (loc Location, ok bool) {
	a = a.Unmap()
	be := binary.BigEndian
	var ri int
	switch {
	case a.Is4():
		v := be.Uint32(a.AsSlice())
		i := sort.Search(tbl.n4, func(i int) bool { return be.Uint32(tbl.s4[i*4:]) > v }) - 1
		if i < 0 {
			return Location{}, false
		}
		ri = int(be.Uint16(tbl.c4[i*2:]))
	case a.Is6():
		b := a.As16()
		v := be.Uint64(b[:8])
		i := sort.Search(tbl.n6, func(i int) bool { return be.Uint64(tbl.s6[i*8:]) > v }) - 1
		if i < 0 {
			return Location{}, false
		}
		ri = int(be.Uint16(tbl.c6[i*2:]))
	default:
		return Location{}, false
	}
	if ri == 0 || ri >= len(tbl.regions) {
		return Location{}, false
	}
	return tbl.regions[ri], true
}

// validateIndexes checks every range points at an existing region. It reads
// the whole table, so it is not run at startup - that would page all 35 MB
// into memory - but by the tests, against the table that gets embedded.
func validateIndexes(t table) error {
	be := binary.BigEndian
	nr := len(t.regions)
	for i := 0; i < t.n4; i++ {
		if int(be.Uint16(t.c4[i*2:])) >= nr {
			return errors.New("ipv4 region index out of range")
		}
	}
	for i := 0; i < t.n6; i++ {
		if int(be.Uint16(t.c6[i*2:])) >= nr {
			return errors.New("ipv6 region index out of range")
		}
	}
	return nil
}

// FindRegion returns the table entry for a region by country and displayed
// name. Only the demo data uses it.
func FindRegion(country, region string) (Location, bool) {
	for _, r := range tbl.regions {
		if r.Country == country && r.Region == region {
			return r, true
		}
	}
	return Location{}, false
}

// Sizes reports the table sizes, for the startup log line.
func Sizes() (regions, v4, v6 int) { return len(tbl.regions), tbl.n4, tbl.n6 }
