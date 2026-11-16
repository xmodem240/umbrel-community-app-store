package asn

import (
	"encoding/json"
	"net/netip"
	"os"
	"testing"
)

// The table is a rebuilt, merged copy of somebody else's database. The only
// question worth asking of it is whether it still answers what the original
// answers, so the vectors are sampled from the source .mmdb by tools/genasn.py
// and carry that database's own answer.
func TestAgainstSourceDatabase(t *testing.T) {
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Skip("no vectors.json - run tools/genasn.py to build one")
	}
	var want []struct {
		Addr string `json:"addr"`
		ASN  uint32 `json:"asn"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) < 100 {
		t.Fatalf("only %d vectors", len(want))
	}

	var wrong int
	for _, c := range want {
		addr, err := netip.ParseAddr(c.Addr)
		if err != nil {
			t.Fatalf("%s: %v", c.Addr, err)
		}
		got, ok := Lookup(addr)
		if !ok || got.Number != c.ASN {
			wrong++
			if wrong <= 5 {
				t.Errorf("%s: got AS%d (%q, ok=%v), want AS%d (%q)",
					c.Addr, got.Number, got.Name, ok, c.ASN, c.Name)
			}
		}
	}
	if wrong > 0 {
		t.Errorf("%d of %d addresses landed in the wrong network", wrong, len(want))
	}
}

func TestNotPublic(t *testing.T) {
	for _, s := range []string{"10.21.0.1", "192.168.1.1", "127.0.0.1", "::1", "fc00::1", "169.254.1.1"} {
		addr := netip.MustParseAddr(s)
		if _, ok := Lookup(addr); ok {
			t.Errorf("%s: a private or local address must not resolve to a network", s)
		}
	}
}

// A well-known one, as a canary: if the table is ever built from the wrong
// input this catches it without needing the vectors.
func TestKnownAddress(t *testing.T) {
	got, ok := Lookup(netip.MustParseAddr("1.1.1.1"))
	if !ok || got.Number != 13335 {
		t.Fatalf("1.1.1.1: got AS%d (%q, ok=%v), want AS13335 Cloudflare", got.Number, got.Name, ok)
	}
	if got.Name == "" {
		t.Error("an operator name is missing")
	}
}

func BenchmarkLookup(b *testing.B) {
	addr := netip.MustParseAddr("195.201.198.22")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Lookup(addr)
	}
}
