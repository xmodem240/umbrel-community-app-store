package main

import (
	"strings"
	"testing"
	"time"
)

// Core versions offering what they never had: the kind says Fake, and why.
// Real peers of their own version stay what they are.
func TestAnachronism(t *testing.T) {
	v2 := []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}
	cases := []struct {
		subver   string
		services []string
		kind     string
		says     string
	}{
		{"/Satoshi:0.14.2/", v2, "Disguised", "Claims: Bitcoin Core 0.14.2\nProbably: not Bitcoin Core 0.14 - it offers encrypted v2 connections, which Core 0.14 did not have (from Core 26)"},
		{"/Satoshi:25.1.0/", v2, "Disguised", "Core 25.1 did not have"},
		{"/Satoshi:0.15.1/", []string{"NETWORK", "WITNESS", "NETWORK_LIMITED"}, "Disguised", "pruned-node service, which Core 0.15"},
		{"/Satoshi:0.20.1/", []string{"NETWORK", "WITNESS", "COMPACT_FILTERS"}, "Disguised", "compact block filters"},
		{"/Satoshi:25.1.0/Knots:20231115/", v2, "Disguised", "Claims: Bitcoin Knots (Core 25.1.0)"},
		{"/Satoshi:0.20.1/Knots:20200815/", []string{"NETWORK", "WITNESS", "COMPACT_FILTERS", "NETWORK_LIMITED"}, "Node (Knots)", ""},
		{"/Satoshi:0.20.99/", []string{"NETWORK", "WITNESS", "COMPACT_FILTERS", "NETWORK_LIMITED"}, "Node (Core)", ""},
		{"/Satoshi:25.99.0/", v2, "Node (Core)", ""},
		{"/Satoshi:0.14.99/", v2, "Disguised", "Core 0.14 did not have"},
		{"/bcoin:v1.0.0-beta.14/", v2, "Disguised", "Claims: bcoin v1.0.0-beta.14"},
		{"/Classic:1.3.4(EB8)/", v2, "Disguised", "never had"},
		{"/Satoshi:31.1.0/", v2, "Node (Core)", ""},
		{"/Satoshi:26.0.0/", v2, "Node (Core)", ""},
		{"/Satoshi:0.21.0/", []string{"NETWORK", "WITNESS", "COMPACT_FILTERS", "NETWORK_LIMITED"}, "Node (Core)", ""},
		{"/Satoshi:0.14.2/", []string{"NETWORK", "WITNESS"}, "Node (Core)", ""},
		{"/Satoshi:31.0.0/", []string{"NETWORK", "WITNESS"}, "Disguised", "lacks NETWORK_LIMITED"},
		{"/Satoshi:0.16.0/", []string{"NETWORK", "WITNESS"}, "Disguised", "since 0.16"},
		{"/Satoshi:0.15.1/", []string{"NETWORK", "WITNESS"}, "Node (Core)", ""},
		{"/Satoshi:0.19.0.1/", []string{"WITNESS"}, "Disguised", "lacks NETWORK_LIMITED"},
		{"/Satoshi:30.2.0/", []string{"WITNESS", "P2P_V2"}, "Disguised", "lacks NETWORK_LIMITED"},
		{"/Floresta:0.9.1/", []string{"WITNESS", "P2P_V2"}, "Node (Floresta)", "Floresta"},
		{"/libbitcoin:4.0.0/", []string{"NETWORK", "WITNESS"}, "Node (libbitcoin)", "libbitcoin"},
	}
	for _, c := range cases {
		got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
			Subver: c.subver, ServicesNames: c.services}})[0]
		if got.Kind != c.kind || !strings.Contains(got.About, c.says) {
			t.Errorf("%s %v: got %q / %q, want %q containing %q", c.subver, c.services, got.Kind, got.About, c.kind, c.says)
		}
	}
}

// A peer naming Core that offers no service at all is not Core: Core always
// offers WITNESS and NETWORK or NETWORK_LIMITED (one node: 6,954 sessions,
// 73% on Amazon, none ever first). nil = Core did not say, which is not this.
func TestCoreOfferingNothing(t *testing.T) {
	no := []string{}
	got := convert([]rawPeer{{Addr: "198.51.100.7:40000", Inbound: true, ConnectionType: "inbound", Subver: "/Satoshi:31.0.0/", ServicesNames: no}})
	if len(got) != 1 || got[0].Kind != fakeWalletKind || got[0].About != "Claims: Bitcoin Core 31.0.0\nProbably: not Bitcoin Core - it offers no services at all; every Core since 0.16 offers NETWORK_LIMITED" {
		t.Fatalf("Core offering nothing: %+v", got)
	}
	for _, svc := range [][]string{nil, {"NETWORK", "WITNESS", "NETWORK_LIMITED"}, {"NETWORK_LIMITED", "WITNESS"}} {
		got := convert([]rawPeer{{Addr: "198.51.100.8:40000", Inbound: true, ConnectionType: "inbound", Subver: "/Satoshi:31.0.0/", ServicesNames: svc}})
		if len(got) != 1 || got[0].Kind != "Node (Core)" {
			t.Errorf("services %v: kind %q, want Node (Core)", svc, got[0].Kind)
		}
	}
	// before 0.16 a pruned Core without bloom filters offered nothing
	got = convert([]rawPeer{{Addr: "198.51.100.10:40000", Inbound: true, ConnectionType: "inbound", Subver: "/Satoshi:0.12.1/", ServicesNames: no}})
	if got[0].Kind != "Node (Core)" {
		t.Errorf("Core 0.12 offering nothing: %q", got[0].Kind)
	}
	// a wallet offering nothing stays a wallet
	got = convert([]rawPeer{{Addr: "198.51.100.9:40000", Inbound: true, ConnectionType: "inbound", Subver: "/bitcoinj:0.16.2/Bitcoin Wallet:9.0/", ServicesNames: no}})
	if got[0].Kind != "Wallet" {
		t.Errorf("wallet offering nothing: %q", got[0].Kind)
	}
}

// Every Core release sends one protocol version: 70017 from 32, 70016 from
// 0.21, 70015 or less before.
func TestProtocolMismatch(t *testing.T) {
	core := []string{"NETWORK", "WITNESS", "NETWORK_LIMITED"}
	cases := []struct {
		subver  string
		version int
		kind    string
		says    string
	}{
		{"/Satoshi:31.0.0/", 70015, "Disguised", "Claims: Bitcoin Core 31.0.0\nProbably: not Bitcoin Core - it speaks protocol 70015, older than any Core since 0.21"},
		{"/Satoshi:0.21.0/", 70015, "Disguised", "protocol 70015"},
		{"/Satoshi:0.13.2/", 70016, "Disguised", "which Core 0.13 never did"},
		{"/Satoshi:25.1.0/Knots:20231115/", 70015, "Disguised", "Claims: Bitcoin Knots"},
		{"/Satoshi:31.1.0/", 70016, "Node (Core)", ""},
		{"/Satoshi:31.99.0/", 70017, "Node (Core)", ""},
		{"/Satoshi:32.0.0/", 70016, "Disguised", "protocol 70016, older than Core 32"},
		{"/Satoshi:32.0.0/", 70017, "Node (Core)", ""},
		{"/Satoshi:0.21.0/", 70016, "Node (Core)", ""},
		{"/Satoshi:0.20.1/", 70015, "Node (Core)", ""},
		{"/Satoshi:0.20.99/", 70016, "Node (Core)", ""}, // a 0.21 development build
		{"/Satoshi:0.13.2/", 70014, "Node (Core)", ""},
		{"/Satoshi:31.1.0/", 0, "Node (Core)", ""},         // Core did not report it
		{"/Satoshi:1.14.5(2x)/", 70015, "Node (Core)", ""}, // not a Core release number: left alone
	}
	for _, c := range cases {
		svc := core
		if strings.HasPrefix(c.subver, "/Satoshi:0.13") {
			svc = []string{"NETWORK", "WITNESS"} // before NETWORK_LIMITED
		}
		got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
			Subver: c.subver, ServicesNames: svc, Version: c.version}})[0]
		if got.Kind != c.kind || !strings.Contains(got.About, c.says) {
			t.Errorf("%s %d: got %q / %q, want %q containing %q", c.subver, c.version, got.Kind, got.About, c.kind, c.says)
		}
	}
}

func TestBehaviourAbout(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	hour := now.Unix() - 3600
	cases := []struct {
		name      string
		subver    string
		transport string
		recv      map[string]int64
		connTime  int64
		want      string
	}{
		{"bitcoinj's ping under Core's name", "/Satoshi:31.1.0/", "v1", map[string]int64{"ping": 32 * 1800}, hour,
			"not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes"},
		{"the same over v2", "/Satoshi:31.1.0/", "v2", map[string]int64{"ping": 29 * 1800}, hour,
			"not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes"},
		{"faster than a second is said as one", "/Satoshi:31.1.0/", "v1", map[string]int64{"ping": 32 * 7200}, hour,
			"not Bitcoin Core - it pings every 1 second, Core every 2 minutes"},
		{"Core's own two minutes", "/Satoshi:31.1.0/", "v2", map[string]int64{"ping": 29 * 31}, hour, ""},
		{"a ping every 15 seconds is still left alone", "/Satoshi:31.1.0/", "v1", map[string]int64{"ping": 32 * 240}, hour, ""},
		{"a development build pinging every 10 seconds is left alone", "/Satoshi:31.99.0/", "v1", map[string]int64{"ping": 32 * 360}, hour, ""},
		{"every 5 seconds is still left alone", "/Satoshi:31.1.0/", "v1", map[string]int64{"ping": 32 * 720}, hour, ""},
		{"every 4 seconds is not", "/Satoshi:31.1.0/", "v1", map[string]int64{"ping": 32 * 900}, hour,
			"not Bitcoin Core - it pings every 4 seconds, Core every 2 minutes"},
		{"too young for a rate", "/Satoshi:31.1.0/", "v1", map[string]int64{"ping": 32 * 30}, now.Unix() - 60, ""},
		{"a crawler asking again and again", "/Satoshi:0.19.0.1/", "v1", map[string]int64{"getaddr": 24 * 58}, now.Unix() - 60,
			"a crawler - it asked for addresses 58 times, Core asks once"},
		{"the same over v2", "/Satoshi:30.0.0/", "v2", map[string]int64{"getaddr": 33 * 3}, hour,
			"not Bitcoin Core - it asked for addresses 3 times, Core asks once"},
		{"Core asks once", "/Satoshi:31.1.0/", "v2", map[string]int64{"getaddr": 33}, hour, ""},
		{"twice over v2 is still left alone", "/Satoshi:31.1.0/", "v2", map[string]int64{"getaddr": 33 * 2}, hour, ""},
		{"twice is still left alone", "/Satoshi:31.1.0/", "v1", map[string]int64{"getaddr": 48}, hour, ""},
		{"Knots runs Core's code", "/Satoshi:29.3.0/Knots:20260507/", "v1", map[string]int64{"ping": 32 * 1800}, hour,
			"not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes"},
		{"bitcoinj under its own name pings as it likes", "/bitcoinj:0.17/", "v1", map[string]int64{"ping": 32 * 1800}, hour, ""},
		{"a transport not settled yet counts nothing", "/Satoshi:31.1.0/", "detecting", map[string]int64{"ping": 32 * 1800, "getaddr": 24 * 9}, hour, ""},
		{"no bytes reported", "/Satoshi:31.1.0/", "v1", nil, hour, ""},
		{"a node on Core before 26 reports no transport: that is v1", "/Satoshi:31.1.0/", "", map[string]int64{"ping": 32 * 1800}, hour,
			"not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes"},
	}
	for _, c := range cases {
		if got := behaviourAbout(c.subver, c.transport, c.recv, c.connTime, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBehaviourMakesCoreDisguised(t *testing.T) {
	conn := time.Now().Unix() - 3600
	got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:31.1.0/",
		Transport: "v1", ConnTime: conn, ServicesNames: []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"},
		BytesRecvPerMsg: map[string]int64{"ping": 32 * 1800}}})[0]
	if got.Kind != fakeWalletKind || got.About != "Claims: Bitcoin Core 31.1.0\nProbably: not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes" {
		t.Errorf("got %q / %q", got.Kind, got.About)
	}
	// The same peer pinging like Core stays a node.
	got = convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:31.1.0/",
		Transport: "v1", ConnTime: conn, ServicesNames: []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"},
		BytesRecvPerMsg: map[string]int64{"ping": 32 * 31, "getaddr": 24}}})[0]
	if got.Kind != "Node (Core)" {
		t.Errorf("an honest Core became %q: %q", got.Kind, got.About)
	}
	// Behaviour comes before a missing service: the crawler is named as one.
	got = convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:0.19.0.1/",
		Transport: "v1", ConnTime: conn, ServicesNames: []string{"WITNESS"},
		BytesRecvPerMsg: map[string]int64{"getaddr": 24 * 58}}})[0]
	if got.Kind != fakeWalletKind || got.About != "Claims: Bitcoin Core 0.19.0.1\nProbably: a crawler - it asked for addresses 58 times, Core asks once"+
		"\nAlso: it lacks NETWORK_LIMITED, which every Core since 0.16 offers" {
		t.Errorf("got %q / %q", got.Kind, got.About)
	}
}

func TestSkippedWtxidrelayIsNotCore(t *testing.T) {
	conn := time.Now().Unix() - 3600
	all := []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}
	core := rawPeer{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Version: 70016,
		Subver: "/Satoshi:28.0.0/", Transport: "v1", ConnTime: conn, ServicesNames: all,
		BytesRecvPerMsg: map[string]int64{"version": 126, "wtxidrelay": 24, "sendaddrv2": 24, "verack": 24, "ping": 32 * 30}}
	// Each case is judged next to this ordinary Core, which sent wtxidrelay.
	if got := convert([]rawPeer{core})[0]; got.Kind != "Node (Core)" {
		t.Errorf("Core sending wtxidrelay: got %q / %q", got.Kind, got.About)
	}
	r := core
	r.BytesRecvPerMsg = map[string]int64{"version": 126, "verack": 24, "ping": 32 * 30}
	if got := convert([]rawPeer{r, core})[0]; got.Kind != fakeWalletKind ||
		got.About != "Claims: Bitcoin Core 28.0.0\nProbably: not Bitcoin Core - it left out wtxidrelay and sendaddrv2 when connecting, which it should have sent" {
		t.Errorf("no wtxidrelay: got %q / %q", got.Kind, got.About)
	}
	// With sendaddrv2 sent, only wtxidrelay is named.
	r.BytesRecvPerMsg = map[string]int64{"version": 126, "sendaddrv2": 24, "verack": 24}
	if got := convert([]rawPeer{r, core})[0]; got.About != "Claims: Bitcoin Core 28.0.0\nProbably: not Bitcoin Core - it left out wtxidrelay when connecting, which it should have sent" {
		t.Errorf("only wtxidrelay missing: got %q", got.About)
	}
	// Knots is Core underneath and sends it too.
	r.Subver = "/Satoshi:29.2.0/Knots:20251110/"
	if got := convert([]rawPeer{r, core})[0]; got.Kind != fakeWalletKind {
		t.Errorf("Knots without wtxidrelay: got %q", got.Kind)
	}
	// Still in its handshake: nothing to judge yet.
	r = core
	r.BytesRecvPerMsg = map[string]int64{"version": 126}
	if got := convert([]rawPeer{r, core})[0]; got.Kind != "Node (Core)" {
		t.Errorf("before verack: got %q", got.Kind)
	}
	// Core before 0.21 never sent it.
	r = core
	r.Subver, r.Version, r.ServicesNames = "/Satoshi:0.20.1/", 70015, []string{"NETWORK", "WITNESS", "NETWORK_LIMITED"}
	r.BytesRecvPerMsg = map[string]int64{"version": 126, "verack": 24}
	if got := convert([]rawPeer{r, core})[0]; got.Kind != "Node (Core)" {
		t.Errorf("Core 0.20: got %q / %q", got.Kind, got.About)
	}
	// No byte counts at all, as from an older Core: no judgement.
	r = core
	r.BytesRecvPerMsg = nil
	if got := convert([]rawPeer{r, core})[0]; got.Kind != "Node (Core)" {
		t.Errorf("no counts: got %q", got.Kind)
	}
}

func TestWtxidrelayNeedsSomeoneToSendIt(t *testing.T) {
	core := rawPeer{Addr: "198.51.100.8:8333", ConnectionType: "outbound-full-relay", Version: 70016,
		Subver: "/Satoshi:28.1.0/", ServicesNames: []string{"NETWORK", "WITNESS", "NETWORK_LIMITED"},
		BytesRecvPerMsg: map[string]int64{"version": 126, "verack": 24}}
	// A node older than 0.21 asks no peer for it, so nobody sends it.
	if got := convert([]rawPeer{core})[0]; got.Kind == fakeWalletKind {
		t.Errorf("no peer sent wtxidrelay, still struck: %q", got.About)
	}
	other := core
	other.Addr = "198.51.100.9:8333"
	other.BytesRecvPerMsg = map[string]int64{"version": 126, "verack": 24, "wtxidrelay": 24, "sendaddrv2": 24}
	if got := convert([]rawPeer{core, other})[0]; got.Kind != fakeWalletKind {
		t.Errorf("another peer sent wtxidrelay, this one did not: got %q", got.Kind)
	}
}

// A peer naming Core that wants transactions, did the handshake and then
// asked for nothing for ten minutes is not Core - but only after every harder
// reason, and not one that may be a block-relay-only connection.
func TestQuietAfterHandshake(t *testing.T) {
	now := time.Now()
	yes, no := true, false
	quiet := map[string]int64{"version": 126, "verack": 24, "feefilter": 32, "pong": 32 * 30}
	asks := map[string]int64{"version": 126, "verack": 24, "pong": 32 * 30, "getheaders": 1061}
	core := func(recv map[string]int64, ago int64) rawPeer {
		return rawPeer{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Version: 70016,
			Subver: "/Satoshi:31.1.0/", Transport: "v1", ConnTime: now.Unix() - ago, RelayTxes: &yes,
			ServicesNames: []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}, BytesRecvPerMsg: recv,
			BytesSentPerMsg: map[string]int64{"inv": 70}}
	}
	if got := convert([]rawPeer{core(quiet, 3600)})[0]; got.Kind != fakeWalletKind ||
		!strings.Contains(got.About, "it asked for nothing after the handshake, not even for addresses") {
		t.Errorf("quiet for an hour: %q / %q", got.Kind, got.About)
	}
	if got := convert([]rawPeer{core(quiet, 120)})[0]; got.Kind != "Node (Core)" {
		t.Errorf("only two minutes: %q", got.Kind)
	}
	if got := convert([]rawPeer{core(asks, 3600)})[0]; got.Kind != "Node (Core)" {
		t.Errorf("asked for headers: %q", got.Kind)
	}
	// A peer that wants no transactions may be a block-relay-only connection,
	// from a node still catching up: nothing to ask for, nothing to say.
	r := core(quiet, 3600)
	r.RelayTxes = &no
	if got := convert([]rawPeer{r})[0]; got.Kind != "Node (Core)" {
		t.Errorf("block-relay-only: %q", got.Kind)
	}
	// Knots runs Core's code and is judged the same.
	k := core(quiet, 3600)
	k.Subver = "/Satoshi:29.3.0/Knots:20260507/"
	if got := convert([]rawPeer{k})[0]; got.Kind != fakeWalletKind {
		t.Errorf("Knots quiet: %q", got.Kind)
	}
	// A harder reason wins: no NETWORK_LIMITED is said, not the quiet.
	h := core(quiet, 3600)
	h.ServicesNames = []string{"NETWORK", "WITNESS"}
	if got := convert([]rawPeer{h})[0]; !strings.Contains(got.About, "lacks NETWORK_LIMITED") {
		t.Errorf("harder reason: %q", got.About)
	}
	// Wasabi on a borrowed Core name stays Wasabi, quiet or not.
	w := core(quiet, 3600)
	w.Subver, w.ServicesNames = "/Satoshi:26.0.0/", []string{}
	if got := convert([]rawPeer{w})[0]; got.Kind == fakeWalletKind && strings.Contains(got.About, "asked for nothing") {
		t.Errorf("quiet Wasabi: %q / %q", got.Kind, got.About)
	}
	// LinkingLion: the line says what it does only once it has been seen doing it.
	lion := core(quiet, 3600)
	lion.Subver, lion.mockLinkingLion = "", true
	if got := convert([]rawPeer{lion})[0]; !strings.HasSuffix(got.About, "; it only listens") {
		t.Errorf("quiet lion: %q", got.About)
	}
	lion = core(quiet, 60)
	lion.Subver, lion.mockLinkingLion = "", true
	if got := convert([]rawPeer{lion})[0]; strings.Contains(got.About, "only listens") {
		t.Errorf("young lion: %q", got.About)
	}
}

// The Bitcore library announces NETWORK only: any more under its name is not it.
func TestBitcoreLibraryOffersOnlyNetwork(t *testing.T) {
	cases := []struct {
		subver   string
		services []string
		kind     string
		says     string
	}{
		{"/bitcore:1.1.0/", []string{"NETWORK"}, "Other", ""},
		{"/bitcore:1.1.0/", []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}, "Disguised", "it offers WITNESS, NETWORK_LIMITED, P2P_V2, which the Bitcore library does not offer by default"},
		{"/bitcore:1.1.0/", []string{"NETWORK", "BLOOM", "WITNESS", "NETWORK_LIMITED"}, "Disguised", "it offers BLOOM, WITNESS, NETWORK_LIMITED"},
		{"/bitcore:8.25.12/", []string{"NETWORK"}, "Other", ""},
	}
	for _, c := range cases {
		got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
			Subver: c.subver, ServicesNames: c.services}})[0]
		if got.Kind != c.kind || !strings.Contains(got.About, c.says) {
			t.Errorf("%s %v: got %q / %q", c.subver, c.services, got.Kind, got.About)
		}
	}
}

// A Core connection on a node that announces nothing is no proof of a fake.
func TestQuietCoreNeedsSomethingOffered(t *testing.T) {
	now := time.Now()
	yes := true
	quiet := map[string]int64{"version": 126, "verack": 24, "ping": 32, "pong": 32 * 40}
	hour := now.Unix() - 3600
	if got := quietCoreAbout("/Satoshi:29.0.0/", &yes, quiet, nil, hour, now); got != "" {
		t.Errorf("nothing offered: %q", got)
	}
	if got := quietCoreAbout("/Satoshi:29.0.0/", &yes, quiet, map[string]int64{"inv": 70}, hour, now); got == "" {
		t.Error("offered and still silent: want the reason")
	}
}

// Core 32 speaks 70017; a Core 22 to 31 that does is none.
func TestProtocol70017(t *testing.T) {
	if got := protocolMismatch("/Satoshi:32.0.0/", 70017); got != "" {
		t.Errorf("32.0.0: %q", got)
	}
	if got := protocolMismatch("/Satoshi:31.1.0/", 70017); got != "not Bitcoin Core - it speaks protocol 70017, which Core 31 never did" {
		t.Errorf("31.1.0: %q", got)
	}
	if got := protocolMismatch("/Satoshi:31.99.0/", 70017); got != "" {
		t.Errorf("31.99 dev build: %q", got)
	}
}

// Its own address where ours belongs backs a disguise; localhost and our
// real address do not.
func TestGivesOwnAddress(t *testing.T) {
	for _, c := range []struct {
		addr, local string
		want        bool
	}{
		{"35.242.187.91:40000", "35.242.187.91:8333", true},
		{"35.242.187.91:40000", "203.0.113.7:8333", false},
		{"35.242.187.91:40000", "127.0.0.1:8333", false},
		{"10.21.21.10:40000", "10.21.21.10:8333", false},
		{"35.242.187.91:40000", "", false},
	} {
		if got := givesOwnAddress(rawPeer{Addr: c.addr, AddrLocal: c.local, Inbound: true}); got != c.want {
			t.Errorf("%s / %s: got %v", c.addr, c.local, got)
		}
	}
	// A wallet offering blocks is Disguised already; the address comes on top.
	p := convert([]rawPeer{{ID: 1, Addr: "35.242.187.91:40000", AddrLocal: "35.242.187.91:8333", ConnectionType: "inbound", Inbound: true,
		Subver: "/bitcoinj:0.14.5/Bitcoin Wallet:5.33/", Version: 70015, ServicesNames: []string{"NETWORK"}}})[0]
	if p.Kind != fakeWalletKind || !strings.Contains(p.About, "Also: "+ownAddressAbout) || p.Tell != "wallet offering blocks \u00b7 gives its own address as ours" {
		t.Fatalf("got %q %q %q", p.Kind, p.About, p.Tell)
	}
	// Alone it decides nothing.
	if p := convert([]rawPeer{{ID: 1, Addr: "35.242.187.91:40000", AddrLocal: "35.242.187.91:8333", ConnectionType: "inbound", Inbound: true,
		Subver: "/Satoshi:31.1.0/", Version: 70016, ServicesNames: []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}}})[0]; p.Kind == fakeWalletKind {
		t.Fatalf("own address alone made it %q", p.Kind)
	}
}
