package main

import (
	"testing"
	"time"
)

func TestLibreRelayAndGarbageman(t *testing.T) {
	bit := []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2", "UNKNOWN[2^29]"}
	for _, c := range []struct{ subver, kind, about string }{
		{"/Satoshi:30.2.0/", "Node (Libre Relay)", "Libre Relay, a fork of Core that also relays transactions Core and Knots filter out"},
		{"/Satoshi:31.1.0/", "Node (Libre Relay)", "Libre Relay, a fork of Core that also relays transactions Core and Knots filter out"},
		{"/Satoshi:29.3.0/", fakeWalletKind, "Claims: Libre Relay\nProbably: Garbageman - a Knots fork that poses as Libre Relay and drops what it gets"},
		{"/Satoshi:29.1.0/", "Node (Libre Relay)", "Libre Relay - or Garbageman, a Knots fork that poses as it"},
		{"/Satoshi:28.1.0/", "Node (Libre Relay)", "Libre Relay - or Garbageman, a Knots fork that poses as it"},
		{"/Satoshi:29.1.0/Knots:20250903/", fakeWalletKind, "Claims: Libre Relay\nProbably: Garbageman - a Knots fork that poses as Libre Relay and drops what it gets"},
	} {
		got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Version: 70016,
			Subver: c.subver, Transport: "v2", ServicesNames: bit}})[0]
		if got.Kind != c.kind || got.About != c.about {
			t.Errorf("%s: got %q / %q", c.subver, got.Kind, got.About)
		}
	}
	// Without the bit it is plain Core.
	got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Version: 70016,
		Subver: "/Satoshi:30.2.0/", Transport: "v2", ServicesNames: bit[:4]}})[0]
	if got.Kind != "Node (Core)" {
		t.Errorf("no bit: got %q", got.Kind)
	}
}

func TestForkBits(t *testing.T) {
	for _, c := range []struct {
		svc        []string
		kind, want string
	}{
		{[]string{"NETWORK", "WITNESS", "UNKNOWN[2^28]"}, "Node (Knots)", "BLAKE2b fork"},
		{[]string{"NETWORK", "UNKNOWN[2^27]", "UNKNOWN[2^28]"}, "Node (Knots)", "BLAKE2b fork"},
		{[]string{"NETWORK", "UNKNOWN[2^27]"}, "Node (Core)", "BIP110 fork"},
		{[]string{"NETWORK", "UNKNOWN[2^27]"}, "Other", "BIP110 fork"},
		{[]string{"NETWORK", "UNKNOWN[2^27]", "UNKNOWN[2^28]"}, "Crawler", ""},
		{[]string{"NETWORK", "UNKNOWN[2^26]"}, "Node (Knots)", ""},
	} {
		if got, why := forkOf(c.svc, c.kind, nil, 0); got != c.want || (got != "") != (why != "") {
			t.Errorf("%v %s: got %q / %q", c.svc, c.kind, got, why)
		}
	}
	// Headers decide when they are known: at the tip it is this chain, stuck
	// thousands of blocks back it is not, and -1 (not yet known) leaves the bit.
	bip110 := []string{"NETWORK", "UNKNOWN[2^27]"}
	for _, c := range []struct {
		synced int64
		want   string
	}{{972597, ""}, {972591, ""}, {972590, "BIP110 fork"}, {961170, "BIP110 fork"}, {-1, "BIP110 fork"}} {
		h := c.synced
		if got, _ := forkOf(bip110, "Other", &h, 972597); got != c.want {
			t.Errorf("synced %d at tip 972597: got %q, want %q", c.synced, got, c.want)
		}
	}
}

// A node catching up takes blocks fast and has not reached the tip; a peer that
// has, or that was handed the same amount over days, is not catching up.
func TestSyncingFrom(t *testing.T) {
	now := time.Now()
	ago := func(min int64) int64 { return now.Unix() - min*60 }
	blocks := func(mb int64) map[string]int64 { return map[string]int64{"block": mb * 1_000_000} }
	at := func(h int64) *int64 { return &h }
	const tip = 973_000
	cases := []struct {
		name    string
		inbound bool
		sent    map[string]int64
		synced  *int64
		conn    int64
		want    int64
	}{
		{"463 MB in twelve minutes, height unknown", true, blocks(463), at(-1), ago(12), 463},
		{"the same, no field at all", true, blocks(463), nil, ago(12), 463},
		{"far behind the tip", true, blocks(463), at(400_000), ago(12), 463},
		{"600 GB taken, now at the tip", true, blocks(600_000), at(tip - 1), ago(15 * 60), 0},
		{"11.9 GB over 2.5 days, 6 blocks behind", true, blocks(11_893), at(tip - 6), ago(3649), 0},
		{"3.8 GB over 2 days, 27 blocks behind", true, blocks(3_831), at(tip - 27), ago(3127), 0},
		{"a day behind and fast", true, blocks(463), at(tip - 150), ago(12), 463},
		{"the same over two days", true, blocks(463), at(-1), ago(2 * 24 * 60), 0},
		{"a few blocks", true, blocks(4), at(-1), ago(5), 0},
		{"we are the one catching up", false, blocks(463), at(-1), ago(12), 0},
		{"no counters", true, nil, at(-1), ago(12), 0},
		{"connection time unknown", true, blocks(463), at(-1), 0, 0},
	}
	for _, c := range cases {
		if got := syncingFrom(c.inbound, c.sent, c.synced, tip, c.conn, now); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	p := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:31.1.0/",
		ServicesNames: []string{"NETWORK", "WITNESS", "NETWORK_LIMITED"}, ConnTime: ago(12), BytesSentPerMsg: blocks(463)}})[0]
	if p.Kind != "Node (Core)" || p.SyncMB != 463 {
		t.Errorf("a Core node syncing: %q, %d MB", p.Kind, p.SyncMB)
	}
}
