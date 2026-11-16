package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Core's private broadcast and python-bitcoinlib say what they are; a peer
// that introduced itself without a name is told apart from one still silent.
func TestOtherAbouts(t *testing.T) {
	cases := []struct {
		subver  string
		version int
		about   string
	}{
		{"/pynode:0.0.1/", 70016, "Probably Bitcoin Core handing over a transaction privately"},
		{"/python-bitcoinlib:0.12.2/", 70016, "python-bitcoinlib"},
		{"", 70016, "Introduced itself without a name"},
		{"", 0, "Has not said what it is"},
		{"/bitcoinj:0.17/", 70013, "bitcoinj, a Java library"},
		{"/dsn.kastel.kit.edu/bitcoin:28.0.0/", 70016, "KIT Karlsruhe's Bitcoin network monitoring"},
		{"/btcwire:0.5.0/:/", 70016, "A program on btcd's Go library that left its own name empty"},
		{"/GlobalNodeMap:2.1/", 70016, "Probably a node map or counter"},
		{"/pycoinnet/", 70016, "Named like pycoinnet"},
		{"/reset-header-agent:1.0/", 70016, "Probably a header client for RESET"},
		{"/RESET-Headers:1.0/", 70016, "Probably a header client for RESET"},
		{"/btcwire:0.5.0/", 70016, "A program on btcd's Go library that set no name of its own"},
		{"/btcwire:0.5.0/peer:1.0.0/", 70016, "Probably a program built from the example code in btcd's Go library"},
		{"/mantle-replay-iouring:0.1.0/", 70016, "Mantle's replay tool"},
		{"/mantle-discover:0.1.0/", 70016, "Mantle's peer finder"},
		{"/mantle-rank-peers:0.1.0/", 70016, "Mantle's speed test"},
		{"/btcwire:0.4.1/Satoshi:0.19/", 70016, "Pool Detective"},
		{"/jwrd.net:0.9.99.99/", 70016, "Probably a node on JWRD Computing's own bitcoind"},
		{"/curl:8.5.0/", 70015, "Names curl, the download tool"},
	}
	for _, c := range cases {
		got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
			Subver: c.subver, Version: c.version, ServicesNames: []string{}}})[0]
		if !strings.HasPrefix(got.About, c.about) {
			t.Errorf("%q %d: about %q, want %q", c.subver, c.version, got.About, c.about)
		}
	}
}

// A name nothing knows still gets a line: a full node's if it offers blocks.
func TestOtherAboutOffersBlocks(t *testing.T) {
	got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
		Subver: "/Dain:0.1.0/", Version: 70016, ServicesNames: []string{"NETWORK", "WITNESS"}}})[0]
	if got.Kind != "Other" || got.About != "Probably a full node under a name its operator chose" {
		t.Errorf("kind %q about %q", got.Kind, got.About)
	}
}

func TestP2aburnAbout(t *testing.T) {
	if got := aboutOf("/p2aburn:1.0/"); !strings.HasPrefix(got, "p2aburn, a bot that sweeps P2A anchors") || kindOf("/p2aburn:1.0/") != "Other" {
		t.Errorf("got %q / %q", kindOf("/p2aburn:1.0/"), got)
	}
}

func TestOtherSaysWhatItDoes(t *testing.T) {
	no, yes := false, true
	for _, c := range []struct {
		svc   []string
		relay *bool
		recv  map[string]int64
		sent  map[string]int64
		want  string
	}{
		{[]string{"NETWORK", "WITNESS"}, &yes, nil, nil, "Probably a full node under a name its operator chose"},
		// It offers blocks but takes transactions and announces none: what it
		// did comes before the guess (/spynode:0.1/, 06.11.2026).
		{[]string{"NETWORK"}, &yes, map[string]int64{"getdata": 41000, "getheaders": 210, "pong": 2100}, map[string]int64{"inv": 9000000, "tx": 7300000, "block": 20000000}, "Fetches transactions from your node and announces none: a node that hears of them elsewhere first, or a program collecting them"},
		// It offers blocks and sends transactions it announced: still the guess
		// (/Dain:0.1.0/, 06.11.2026).
		{[]string{"NETWORK", "WITNESS"}, &yes, map[string]int64{"inv": 400000, "tx": 9100000, "getdata": 60000}, map[string]int64{"inv": 3000000, "tx": 6700000}, "Probably a full node under a name its operator chose"},
		{[]string{}, &yes, map[string]int64{"tx": 90 * 3}, nil, "Pushes transactions without announcing them"},
		{[]string{}, &yes, map[string]int64{"tx": 90, "inv": 61}, nil, "Software with a name we don't recognise"},
		{[]string{}, &no, map[string]int64{"getheaders": 1053, "sendheaders": 24}, nil, "Asks for headers and wants no transactions: a program that tracks the chain"},
		{[]string{}, &no, map[string]int64{"getaddr": 24}, nil, "Asks for addresses, not for headers or blocks: a crawler"},
		{[]string{}, &yes, map[string]int64{"getaddr": 24, "getdata": 1117166, "pong": 15936}, map[string]int64{"inv": 17521264, "tx": 5292757}, "Fetches transactions from your node and announces none: a program collecting them"},
		// A VerifiedX validator before it had a name here: asks for blocks,
		// is sent blocks, no transactions.
		{[]string{}, &yes, map[string]int64{"getdata": 1647, "getheaders": 354039}, map[string]int64{"block": 21269099, "inv": 565744}, "Software with a name we don't recognise"},
		{[]string{}, &no, nil, nil, "Software with a name we don't recognise"},
	} {
		if got := otherAbout(c.svc, c.relay, c.recv, c.sent, 0, time.Now()); got != c.want {
			t.Errorf("%v %v: got %q, want %q", c.svc, c.recv, got, c.want)
		}
	}
}

// An unknown name that stays and asks for nothing is said to only listen,
// ahead of whatever its services suggest.
func TestOtherAboutOnlyListens(t *testing.T) {
	now := time.Now()
	yes, no := true, false
	quiet := map[string]int64{"version": 126, "verack": 24, "ping": 32, "pong": 32 * 40, "sendaddrv2": 24}
	hour := now.Unix() - 3600
	if got := otherAbout(nil, &yes, quiet, map[string]int64{"inv": 120}, hour, now); got != "Only listens: hears new transactions, fetches none" {
		t.Errorf("wants transactions: %q", got)
	}
	// Nothing was announced to it, so it cannot be said to hear anything.
	if got := otherAbout(nil, &yes, quiet, nil, hour, now); got != "Only listens: asks for nothing" {
		t.Errorf("nothing announced: %q", got)
	}
	if got := otherAbout(nil, &no, quiet, nil, hour, now); got != "Only listens: wants no transactions, asks for nothing" {
		t.Errorf("wants none: %q", got)
	}
	// Listing NETWORK does not make a silent peer a full node.
	if got := otherAbout([]string{"NETWORK"}, &yes, quiet, nil, hour, now); !strings.Contains(got, "Only listens") {
		t.Errorf("NETWORK but silent: %q", got)
	}
	// Not long enough yet, or it did ask: the older lines stand.
	if got := otherAbout(nil, &yes, quiet, nil, now.Unix()-60, now); strings.Contains(got, "only listens") {
		t.Errorf("one minute: %q", got)
	}
	asks := map[string]int64{"version": 126, "verack": 24, "getaddr": 24}
	if got := otherAbout(nil, &yes, asks, nil, hour, now); got != "Asks for addresses, not for headers or blocks: a node already in sync, or a crawler" {
		t.Errorf("collects addresses: %q", got)
	}
}

// What unknown software did, said plainly: asking for the mempool, downloading
// blocks, wanting headers announced.
func TestOtherAboutWhatItDid(t *testing.T) {
	now := time.Now()
	yes, no := true, false
	cases := []struct {
		name  string
		relay *bool
		recv  map[string]int64
		sent  map[string]int64
		want  string
	}{
		{"mempool", &yes, map[string]int64{"version": 126, "verack": 24, "mempool": 24}, nil,
			"Asks for our mempool"},
		{"asks for headers", &no, map[string]int64{"version": 126, "getheaders": 1061}, nil,
			"Asks for headers and wants no transactions: a block-only connection, or a program that tracks the chain"},
		{"only wants them announced", &no, map[string]int64{"version": 126, "sendheaders": 24}, nil,
			"Asks for headers and wants no transactions: a block-only connection, or a program that tracks the chain"},
	}
	for _, c := range cases {
		if got := otherAbout(nil, c.relay, c.recv, c.sent, 0, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A peer with a version but no name says what it did next.
func TestNoAgentAbout(t *testing.T) {
	now := time.Now()
	hour := now.Unix() - 3600
	cases := []struct {
		name string
		recv map[string]int64
		conn int64
		want string
	}{
		{"a full node without a name", map[string]int64{"version": 126, "verack": 24, "getheaders": 1061, "getaddr": 24, "sendcmpct": 33}, hour,
			"Asks for headers and addresses, like a full node does"},
		{"addresses only", map[string]int64{"version": 126, "verack": 24, "getaddr": 24, "pong": 32}, hour,
			"Asks for addresses, not for headers or blocks"},
		{"quiet for an hour", map[string]int64{"version": 126, "verack": 24, "pong": 32 * 30}, hour,
			"Introduced itself, then asked for nothing"},
		{"just arrived", map[string]int64{"version": 126, "verack": 24}, now.Unix() - 5,
			"Introduced itself without a name; nothing it did tells what it is"},
		{"no counters", nil, hour,
			"Introduced itself without a name; nothing it did tells what it is"},
	}
	for _, c := range cases {
		if got := noAgentAbout(c.recv, c.conn, now); got != c.want {
			t.Errorf("%s: got %q", c.name, got)
		}
	}
}

// A fixed sentence says what a name stands for; what the peer did in the
// session is still said under it, and a session that showed nothing adds
// nothing.
func TestFixedSentenceKeepsTheSession(t *testing.T) {
	yes := true
	for _, ua := range []string{"/curl:8.5.0/", "urbit", "/dexcash-nodes:0.1.0/", "/bip157-fetch:0.1/"} {
		got := convertAt([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: ua, Version: 70016,
			ServicesNames: []string{}, RelayTxes: &yes, BytesRecvPerMsg: map[string]int64{"mempool": 24}}}, time.Now())[0]
		if !strings.Contains(got.About+"\n"+got.Seen, "Asks for our mempool") {
			t.Errorf("%s: the session is lost: %q / %q", ua, got.About, got.Seen)
		}
		if strings.Contains(got.About, "In this session") {
			t.Errorf("%s: the session line is in the about again: %q", ua, got.About)
		}
		quiet := convertAt([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: ua, Version: 70016,
			ServicesNames: []string{}, RelayTxes: &yes}}, time.Now())[0]
		if quiet.Seen != "" || strings.Contains(quiet.About, unknownAbout) && aboutOf(ua) != "" {
			t.Errorf("%s: a quiet session added a line: %q / %q", ua, quiet.About, quiet.Seen)
		}
	}
}

func TestObservedPart(t *testing.T) {
	for in, want := range map[string]string{
		"Only listens: hears new transactions, fetches none": "Hears new transactions, fetches none",
		"Only listens: asks for nothing":                     "Asks for nothing",
		"Asks for headers and wants no transactions: a block-only connection, or a program that tracks the chain": "Asks for headers and wants no transactions",
		"Asks for our mempool":                        "Asks for our mempool",
		"Pushes transactions without announcing them": "Pushes transactions without announcing them",
	} {
		if got := observedPart(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// The server, not the wording, tells the page that the line above already
// says the peer wants no transactions.
func TestNoTxSaid(t *testing.T) {
	no, yes := false, true
	headers := map[string]int64{"getheaders": 2000}
	for _, c := range []struct {
		ua    string
		relay *bool
		want  bool
	}{
		{"/reset-header-relay:0.1/", &no, true}, // fixed sentence, session line under it
		{"/some-tool:0.1/", &no, true},          // the session line is the about
		{"/some-tool:0.1/", &yes, false},        // it wants transactions
	} {
		p := convertAt([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: c.ua, Version: 70016,
			ServicesNames: []string{}, RelayTxes: c.relay, BytesRecvPerMsg: headers}}, time.Now())[0]
		if p.NoTxSaid != c.want || c.want && !strings.Contains(strings.ToLower(p.About+" "+p.Seen), "wants no transactions") {
			t.Errorf("%s relay %v: no_tx_said %v, about %q, seen %q", c.ua, *c.relay, p.NoTxSaid, p.About, p.Seen)
		}
	}
}

// Every rule has a label that fits one line, 1000 px wide too, and keeps the rule's hedge: a
// sentence that says "Probably" or "Names" does not turn into a fact.
func TestLabelsFitAndKeepTheHedge(t *testing.T) {
	for _, r := range aboutRules {
		if r.label == "" || len(r.label) > 25 {
			t.Errorf("%q: label %q (%d chars)", r.text, r.label, len(r.label))
		}
		for _, hedge := range []string{"Probably", "Names", "Named"} {
			if strings.HasPrefix(r.text, hedge) && !strings.HasPrefix(r.label, hedge) && !strings.HasPrefix(r.label, "Borrowed") {
				t.Errorf("%q: label %q drops %q", r.text, r.label, hedge)
			}
		}
	}
}

// Label and chip belong to the rule's sentence: a disguise that puts its own
// about in its place takes them away, a session line keeps its own label.
func TestLabelGoesWithItsSentence(t *testing.T) {
	yes := true
	conv := func(ua string, svc []string, recv map[string]int64) Peer {
		return convertAt([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: ua, Version: 70016,
			ServicesNames: svc, RelayTxes: &yes, BytesRecvPerMsg: recv}}, time.Now())[0]
	}
	if p := conv("/reset-header-relay:0.1/", []string{}, nil); p.Label != "Probably a RESET client" || p.Name != "RESET" {
		t.Errorf("RESET: label %q, name %q", p.Label, p.Name)
	}
	// Schildbach's wallet offering blocks is a disguise: its about is the reasons.
	if p := conv("/bitcoinj:0.14.4/Bitcoin Wallet:5.18/", []string{"NETWORK", "WITNESS"}, nil); p.Kind != "Disguised" || p.Label != "" || p.Seen != "" {
		t.Errorf("disguised wallet: kind %q, label %q, seen %q", p.Kind, p.Label, p.Seen)
	}
	if p := conv("/some-tool:0.1/", []string{}, map[string]int64{"mempool": 24}); p.Label != "Asks for our mempool" || p.Seen != "" {
		t.Errorf("unknown tool: label %q, seen %q", p.Label, p.Seen)
	}
	quiet := rawPeer{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: "/some-tool:0.1/", Version: 70016,
		ServicesNames: []string{}, RelayTxes: &yes, ConnTime: time.Now().Unix() - 7200, BytesRecvPerMsg: map[string]int64{"version": 126, "verack": 24}}
	if p := convertAt([]rawPeer{quiet}, time.Now())[0]; p.Label != "Only listens" {
		t.Errorf("listener: label %q, about %q", p.Label, p.About)
	}
	// LinkingLion's range puts its own about in place: the listener's label goes.
	quiet.mockLinkingLion = true
	if p := convertAt([]rawPeer{quiet}, time.Now())[0]; p.Label != "" || !strings.Contains(p.About, "LinkingLion") {
		t.Errorf("LinkingLion: label %q, about %q", p.Label, p.About)
	}
}

// Every line sessionLine can give has a short form for the list, and the
// short form fits one line like the rules' labels.
func TestSessionLinesHaveShortForms(t *testing.T) {
	src, err := os.ReadFile("about.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src[strings.Index(string(src), "func sessionLine("):])
	body = body[:strings.Index(body, "\n}\n")]
	n := 0
	for _, m := range regexp.MustCompile(`, "([^"]*)", (true|false)(\n|$)`).FindAllStringSubmatch(body, -1) {
		n++
		if m[1] == "" || len(m[1]) > 25 {
			t.Errorf("short form %q", m[1])
		}
	}
	if n != 11 {
		t.Errorf("found %d short forms, want 11 - did a line lose its short form?", n)
	}
}

// A pool node shows its pool on a line of its own: the label does not say
// the name again.
func TestPoolLabelLeavesTheNameToItsLine(t *testing.T) {
	yes := true
	for addr, n := range poolNodes {
		if !addr.Is4() {
			continue
		}
		p := convertAt([]rawPeer{{Addr: addr.String() + ":8333", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:31.0.0/",
			Version: 70016, ServicesNames: []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}, RelayTxes: &yes}}, time.Now())[0]
		if p.Pool != n.pool || p.Label != "Own Bitcoin node" {
			t.Errorf("%s: pool %q, label %q", addr, p.Pool, p.Label)
		}
	}
}

// Hemi's code with no program name, as Core shows it (the "+" dropped) and as
// sent; other empty names are left alone.
func TestHemiUnnamed(t *testing.T) {
	for ua, want := range map[string]bool{
		"/:1.1.0-dev539f4e33c/":  true,
		"/:1.1.0-dev+c1771608d/": true,
		"/:2.1.0-dev/":           false,
		"//":                     false,
		"/:1.0/":                 false,
	} {
		if got := hemiUnnamed.MatchString(ua); got != want {
			t.Errorf("%q: %v, want %v", ua, got, want)
		}
	}
}

// A Knots node, so it is not in the table above: that one has no services.
func TestKnotsSHA256About(t *testing.T) {
	if got := aboutOf("/Satoshi:31.1.0/KnotsSHA256:20261018/"); !strings.HasPrefix(got, "Knots (SHA256), paulscode's build") {
		t.Errorf("KnotsSHA256: %q", got)
	}
}

func TestFutureBitApolloAbout(t *testing.T) {
	ua := "/Satoshi:28.1.0(FutureBit-Apollo-Node)/"
	if got := aboutOf(ua); !strings.HasPrefix(got, "Labelled by FutureBit Apollo") || kindOf(ua) != "Node (Core)" {
		t.Errorf("about %q, kind %q", got, kindOf(ua))
	}
}

func TestNodeSetupLabels(t *testing.T) {
	for ua, want := range map[string]string{
		"/Satoshi:31.0.0(Samourai Dojo 1.29.3)/":         "Labelled by Samourai Dojo",
		"/Satoshi:29.0.0(RoninDojo 2.3.0)/":              "Labelled RoninDojo",
		"/Satoshi:31.1.0(MiniBolt node)/":                "Labelled by MiniBolt",
		"/Satoshi:29.0.0(MiniBolt_Node)/":                "Labelled by MiniBolt",
		"/Satoshi:32.0.0(Official MiniBolt node)/":       "Labelled by MiniBolt",
		"/Satoshi:32.1.0(21M; MiniBolt node)/":           "Labelled by MiniBolt",
		"/Satoshi:29.4.1(MiniBolt node)/Knots:20260508/": "Labelled by MiniBolt",
		"/Satoshi:29.0.0(bitcore)/":                      "Labelled bitcore",
	} {
		if got := aboutOf(ua); !strings.HasPrefix(got, want) {
			t.Errorf("%s: about %q", ua, got)
		}
	}
	if k := kindOf("/Satoshi:29.4.1(MiniBolt node)/Knots:20260508/"); k != "Node (Knots)" {
		t.Errorf("MiniBolt on Knots: kind %q", k)
	}
}
