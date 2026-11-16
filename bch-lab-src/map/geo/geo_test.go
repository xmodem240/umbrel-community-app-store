package geo

import (
	"encoding/binary"
	"encoding/json"
	"net/netip"
	"os"
	"testing"
)

// vectors.json holds addresses sampled from the DB-IP .mmdb files by
// tools/gengeo.py, with the country and region a direct lookup in the .mmdb
// gives for them (region names folded the same way the table folds them).
func TestAgainstSourceDatabase(t *testing.T) {
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vs [][3]string
	if err := json.Unmarshal(b, &vs); err != nil {
		t.Fatal(err)
	}
	if len(vs) < 1000 {
		t.Fatalf("only %d vectors", len(vs))
	}
	bad := 0
	for _, v := range vs {
		loc, ok := Lookup(netip.MustParseAddr(v[0]))
		if !ok || loc.Country != v[1] || loc.Region != v[2] {
			bad++
			if bad <= 10 {
				t.Errorf("%s: got %q/%q (ok=%v) want %q/%q", v[0], loc.Country, loc.Region, ok, v[1], v[2])
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d vectors wrong", bad, len(vs))
	}
}

func TestKnownRows(t *testing.T) {
	// Expected values read from dbip-city-ipv4/ipv6.mmdb (2026-06-05 build).
	for ip, want := range map[string][2]string{
		"1.0.0.1":        {"AU", "Queensland"},
		"1.0.2.200":      {"CN", "Fujian"},
		"8.8.8.8":        {"US", "California"},
		"2a01:4f8::1":    {"DE", "Bavaria"},
		"2600:1f18::1":   {"US", "Virginia"},
		"::ffff:1.0.0.1": {"AU", "Queensland"},
	} {
		loc, ok := Lookup(netip.MustParseAddr(ip))
		if !ok || loc.Country != want[0] || loc.Region != want[1] {
			t.Errorf("%s: got %q/%q want %q/%q", ip, loc.Country, loc.Region, want[0], want[1])
		}
		if loc.Lat == 0 && loc.Lon == 0 {
			t.Errorf("%s: region without a position", ip)
		}
	}
	for _, ip := range []string{"10.21.0.1", "192.168.178.124", "fd00::1"} {
		if loc, ok := Lookup(netip.MustParseAddr(ip)); ok {
			t.Errorf("%s: private address located in %+v", ip, loc)
		}
	}
}

// The name folding must collapse the duplicates seen in the data.
func TestRegionNamesFolded(t *testing.T) {
	count := map[string]map[string]bool{}
	for _, r := range tbl.regions {
		if count[r.Country] == nil {
			count[r.Country] = map[string]bool{}
		}
		if r.Region != "" {
			count[r.Country][r.Region] = true
		}
	}
	for cc, want := range map[string]int{"DE": 16, "AU": 8} {
		if got := len(count[cc]); got != want {
			names := []string{}
			for n := range count[cc] {
				names = append(names, n)
			}
			t.Errorf("%s: %d regions, want %d: %v", cc, got, want, names)
		}
	}
	if !count["DE"]["Berlin"] || count["DE"]["State of Berlin"] {
		t.Errorf("DE should list Berlin, not State of Berlin")
	}
}

func TestIndexesValid(t *testing.T) {
	if err := validateIndexes(tbl); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < tbl.n4; i++ {
		if binary.BigEndian.Uint32(tbl.s4[(i-1)*4:]) >= binary.BigEndian.Uint32(tbl.s4[i*4:]) {
			t.Fatalf("ipv4 starts not strictly ascending at %d", i)
		}
	}
	for i := 1; i < tbl.n6; i++ {
		if binary.BigEndian.Uint64(tbl.s6[(i-1)*8:]) >= binary.BigEndian.Uint64(tbl.s6[i*8:]) {
			t.Fatalf("ipv6 starts not strictly ascending at %d", i)
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := parse([]byte("nope")); err == nil {
		t.Fatal("expected error")
	}
	if _, err := parse(raw[:len(raw)-1]); err == nil {
		t.Fatal("expected error for truncated table")
	}
}

func BenchmarkLookup(b *testing.B) {
	a := netip.MustParseAddr("2a01:4f8::1")
	for i := 0; i < b.N; i++ {
		Lookup(a)
	}
}
