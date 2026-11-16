// Package asn maps an IP address to the network that announces it - the
// autonomous system number and its operator's name - using the table in
// asn.bin, built by tools/genasn.py from ip-location-db's ASN database
// (RouteViews + DB-IP).
//
// The map answers where a peer is; this answers whose machine it is, and for a
// node the second question is often the more useful one. Three peers in three
// countries can still be three rented machines at one operator, and nothing
// about the address says so: measured on one real node, three of eight manual
// peers sat in 195.201.0.0/16, 188.40.0.0/16 and 65.109.0.0/16 - three
// different networks, one company.
//
// Same shape as the geo package: the table is embedded and searched in place,
// so nothing is decoded into the heap except the operator names, and the kernel
// pages in only the few kilobytes a binary search actually touches.
package asn

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"net/netip"
	"sort"
)

//go:embed asn.bin
var raw []byte

// Info is what a lookup returns. Number is 0 when no network announces the
// address - a gap in the table, not a failure.
type Info struct {
	Number uint32
	Name   string
}

type table struct {
	infos  []Info
	n4     int
	s4, i4 []byte
	n6     int
	s6, i6 []byte
}

var tbl table

func init() {
	t, err := parse(raw)
	if err != nil {
		panic("asn.bin: " + err.Error())
	}
	tbl = t
}

func parse(b []byte) (table, error) {
	var t table
	be := binary.BigEndian
	if len(b) < 10 || string(b[:6]) != "PMASN1" {
		return t, errors.New("bad magic")
	}
	p := 6
	need := func(n int) error {
		if n < 0 || p+n > len(b) {
			return errors.New("truncated")
		}
		return nil
	}

	if err := need(4); err != nil {
		return t, err
	}
	n := int(be.Uint32(b[p:]))
	p += 4
	t.infos = make([]Info, n)
	for i := 0; i < n; i++ {
		if err := need(5); err != nil {
			return t, err
		}
		num := be.Uint32(b[p:])
		ln := int(b[p+4])
		p += 5
		if err := need(ln); err != nil {
			return t, err
		}
		t.infos[i] = Info{Number: num, Name: string(b[p : p+ln])}
		p += ln
	}

	block := func(width int) (int, []byte, []byte, error) {
		if err := need(4); err != nil {
			return 0, nil, nil, err
		}
		count := int(be.Uint32(b[p:]))
		p += 4
		if err := need(count * width); err != nil {
			return 0, nil, nil, err
		}
		starts := b[p : p+count*width]
		p += count * width
		if err := need(count * 4); err != nil {
			return 0, nil, nil, err
		}
		idx := b[p : p+count*4]
		p += count * 4
		return count, starts, idx, nil
	}

	var err error
	if t.n4, t.s4, t.i4, err = block(4); err != nil {
		return t, err
	}
	if t.n6, t.s6, t.i6, err = block(8); err != nil {
		return t, err
	}
	return t, nil
}

// Lookup returns the network announcing addr. ok is false for an address no
// network announces, and for anything that is not a public IP.
func Lookup(addr netip.Addr) (Info, bool) {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return Info{}, false
	}
	var at int
	if addr.Is4() {
		b := addr.As4()
		key := binary.BigEndian.Uint32(b[:])
		at = sort.Search(tbl.n4, func(i int) bool {
			return binary.BigEndian.Uint32(tbl.s4[i*4:]) > key
		})
		if at == 0 {
			return Info{}, false
		}
		return info(binary.BigEndian.Uint32(tbl.i4[(at-1)*4:]))
	}
	b := addr.As16()
	key := binary.BigEndian.Uint64(b[:8])
	at = sort.Search(tbl.n6, func(i int) bool {
		return binary.BigEndian.Uint64(tbl.s6[i*8:]) > key
	})
	if at == 0 {
		return Info{}, false
	}
	return info(binary.BigEndian.Uint32(tbl.i6[(at-1)*4:]))
}

func info(i uint32) (Info, bool) {
	if int(i) >= len(tbl.infos) {
		return Info{}, false
	}
	v := tbl.infos[i]
	if v.Number == 0 {
		return Info{}, false
	}
	return v, true
}

// Size reports what the embedded table holds, for the startup log line.
func Size() (networks, v4, v6 int) {
	return len(tbl.infos) - 1, tbl.n4, tbl.n6
}
