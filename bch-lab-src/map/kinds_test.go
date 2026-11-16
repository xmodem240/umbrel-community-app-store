package main

import (
	"strings"
	"testing"
)

func TestClientsVerifiedOn29October(t *testing.T) {
	for _, c := range []struct {
		subver string
		kind   string
		relays bool
		about  string
	}{
		{"/BitcoinRocks:31.1.1/", "Node (BitcoinRocks)", true, "RocksDB"},
		{"/bip110-observer:0.1/", "Crawler", false, "BIP-110 Observer"},
		{"/EMPORION:0.3/", "Light client", false, "OpenTimestamps"},
		{"/SYMMARTYS:0.3/", "Light client", false, "formerly EMPORION"},
		{"/FavillaKEY:0.1/", "Other", true, "Favilla KEY"},
		// Operator comments must not turn a Core node into a light client.
		{"/Satoshi:31.1.0(EMPORION:0.3)/", "Node (Core)", true, ""},
		{"/Satoshi:31.1.0(SYMMARTYS:0.3)/", "Node (Core)", true, ""},
		// Similar names are not evidence for a match.
		{"/EMPORION-test:0.3/", "Other", true, ""},
		{"/BitcoinRocksTest:31.1.1/", "Other", true, ""},
	} {
		if got := kindOf(c.subver); got != c.kind {
			t.Errorf("%s: kind %q, want %q", c.subver, got, c.kind)
		}
		if got := relaysBlocks(c.subver); got != c.relays {
			t.Errorf("%s: relays %v, want %v", c.subver, got, c.relays)
		}
		if got := aboutOf(c.subver); !strings.Contains(got, c.about) || c.about == "" && got != "" {
			t.Errorf("%s: about %q, want %q", c.subver, got, c.about)
		}
	}
}

func TestKindOf(t *testing.T) {
	for subver, want := range map[string]string{
		"/Satoshi:31.1.0/":                         "Node (Core)",
		"/Satoshi:29.3.0/Knots:20260507/":          "Node (Knots)",
		"/bitcoinj:0.16.2/Bitcoin Wallet:9.26/":    "Wallet",
		"/breadwallet:1.3.5/":                      "Wallet",
		"/btcwire:0.5.0/neutrino:0.17.1/":          "Lightning wallet",
		"/Rust BIP-157:0.6.0/rust-bitcoin:0.32.8/": "Light client",
		// Seen on a real node, all of them "Other" before.
		"/bitcoin-seeder:0.01/":          "Crawler",
		"/btc-range-scan:0.1.0/":         "Crawler",
		"/BTC-Nodes:2026-09-24/Sonar/":   "Crawler",
		"/census:0.1.7/":                 "Crawler",
		"/btc-node-observatory:0.1.0/":   "Crawler",
		"/go-bitnode-monitor:firstseen/": "Crawler",
		"/atlas-scout:0.1/":              "Crawler",
		"nebula/2.4.1-85b3e43":           "Crawler",
		"/onlytwentyone-crawler:0.1/":    "Crawler",
		// The whole reason the observed flags exist: a zero in place of the o,
		// seen from rented machines in five regions. It must not come out as
		// Core. It was "Other" until the Fake kind existed to say what it is.
		"/Sat0shi:31.0.0/":                              "Disguised",
		"/Satoshi2:0.18.3/":                             "Disguised",
		"/SatoshiX:0.18.0/":                             "Disguised",
		"Satoshi:22.0.0":                                "Disguised",
		"/Satoshi:22.0.0/":                              "Node (Core)",
		"/Floresta:0.9.1/mandacaru:0.15.2/":             "Node (Floresta)",
		"/electrs:0.11.1/":                              "Indexer",
		"/Metrika-Bitnodes:0.1/":                        "Crawler",
		"/bitnodes.io:0.3/":                             "Crawler",
		"/GlobalNodeMap:2.1/":                           "Crawler",
		"/dsn.tm.kit.edu/bitcoin:0.9.99/":               "Research scanner",
		"/ckp2p:2.0/":                                   "Pool node",
		"/CKCoinD:31.1.0/":                              "Pool node",
		"/rust-bitcoin:0.18/bluematt-tokio-client:0.1/": "Crawler",
		// The operator's own word, in the comment part - above the software.
		"/Satoshi:29.1.0(PyBLOCK-POOL)/Knots:20250903/": "Pool node",
		"/Satoshi:31.0.0(pool)/":                        "Pool node",
		// As it arrives on a real node, with a URL after the Knots part.
		"/Satoshi:29.1.0(PyBLOCK-POOL)/Knots:20250903/https://pyblock.xyz:8443/": "Pool node",
		// PyBLOCK's node without the pool: a Knots node, not a pool.
		"/Satoshi:29.4.2(PyBLOCK-BIP110)/Knots:20260508/": "Node (Knots)",
		// "pool" outside the brackets is not a claim: this is an indexer.
		"/mempool:3.0.0/electrs:0.10.0/": "Indexer",
		// "mempool" in the brackets is a node's label, not a pool (seen on a real node)
		"/Satoshi:29.4.2(mempool.guide)/Knots:20260508rc2/": "Node (Knots)",
		"/mempool/":                   "Indexer",
		"/mempool.space:1.0/":         "Indexer",
		"/Bitcoin ABC:0.14.5(EB8.0)/": "Other chain",
		// A word in the brackets is the operator's label, not what the peer is:
		// these are Core nodes, not crawlers, scanners or indexers.
		"/Satoshi:29.0.0(my-monitor)/":       "Node (Core)",
		"/Satoshi:28.1.0(watchtower)/":       "Node (Core)",
		"/Satoshi:27.0.0(uni-mainz)/":        "Node (Core)",
		"/Satoshi:29.0.0(electrum-backend)/": "Node (Core)",
		"/Floresta:0.9.1/":                   "Node (Floresta)",
		"/btcwire:0.5.0/hemi-soak:1.0/":      "Other",
		"":                                   noAgentKind,
	} {
		if got := kindOf(subver); got != want {
			t.Errorf("%q: got %q want %q", subver, got, want)
		}
	}
}

// Knots contains "Satoshi" as well, so the order of the rules decides this
// one. A rule moved above Knots would break it silently.
func TestKnotsBeatsCore(t *testing.T) {
	if got := kindOf("/Satoshi:29.1.0/Knots:20250903/"); got != "Node (Knots)" {
		t.Errorf("got %q, want Node (Knots)", got)
	}
}

func TestObservedFlags(t *testing.T) {
	no, yes := false, true
	minusOne, height := int64(-1), int64(967308)

	cases := []struct {
		name                                string
		raw                                 rawPeer
		noServices, noTxRelay, chainUnknown bool
	}{
		{"nothing reported", rawPeer{}, false, false, false},
		{"a full node", rawPeer{
			ServicesNames: []string{"NETWORK", "WITNESS"}, RelayTxes: &yes, SyncedHeaders: &height,
		}, false, false, false},
		{"offers nothing, wants nothing, chain unknown", rawPeer{
			Version: 70016, ServicesNames: []string{}, RelayTxes: &no, SyncedHeaders: &minusOne,
		}, true, true, true},
		// Still in its handshake: no version yet, so no services listed either.
		// That is not the peer offering nothing.
		{"handshake not done", rawPeer{ServicesNames: []string{}, RelayTxes: &no, SyncedHeaders: &minusOne}, false, false, false},
	}
	for _, c := range cases {
		c.raw.Addr = "1.0.0.1:8333"
		c.raw.ConnectionType = "inbound"
		c.raw.Inbound = true
		got := convert([]rawPeer{c.raw})
		if len(got) != 1 {
			t.Fatalf("%s: got %d peers", c.name, len(got))
		}
		p := got[0]
		if p.NoServices && !p.NoRelay {
			t.Errorf("%s: offers nothing but is not marked as unable to deliver", c.name)
		}
		if p.NoServices != c.noServices || p.NoTxRelay != c.noTxRelay || p.ChainUnknown != c.chainUnknown {
			t.Errorf("%s: got no_services=%v no_tx=%v chain_unknown=%v, want %v/%v/%v",
				c.name, p.NoServices, p.NoTxRelay, p.ChainUnknown, c.noServices, c.noTxRelay, c.chainUnknown)
		}
	}
}

// The red mark in the table, and the red pill in Bitcoin Lab, come from this
// one answer. It has to stay the same in both apps.
func TestRelaysBlocks(t *testing.T) {
	for subver, want := range map[string]bool{
		"/Satoshi:31.1.0/":                      true,
		"/Satoshi:29.3.0/Knots:20260507/":       true,
		"/ckp2p:2.0/":                           true,
		"/Floresta:0.9.1/":                      true,
		"/Sat0shi:31.0.0/":                      true, // unrecognised is not the same as known bad
		"":                                      true, // no user agent at all: we do not know
		"/bitcoinj:0.16.2/Bitcoin Wallet:9.26/": false,
		"/breadwallet:1.3.5/":                   false,
		"/btcwire:0.5.0/neutrino:0.17.1/":       false,
		"/electrs:0.11.1/":                      false,
		"/Metrika-Bitnodes:0.1/":                false,
		"/dsn.tm.kit.edu/bitcoin:0.9.99/":       false,
		"/Bitcoin ABC:0.14.5(EB8.0)/":           false,
	} {
		if got := relaysBlocks(subver); got != want {
			t.Errorf("%q: relaysBlocks = %v, want %v", subver, got, want)
		}
	}
}

// Your own electrs reaches Core from inside Umbrel's network, and says so.
func TestOwnIndexer(t *testing.T) {
	got := convert([]rawPeer{{Addr: "10.21.21.10:40216", ConnectionType: "inbound", Inbound: true,
		Network: "not_publicly_routable", Subver: "/electrs:0.11.1/"}})[0]
	if got.Kind != "Indexer" || got.About != "Probably your own electrs 0.11.1 on this Umbrel" || got.App != "electrs" {
		t.Errorf("got %q / %q / %q", got.Kind, got.About, got.App)
	}
	got = convert([]rawPeer{{Addr: "10.21.22.199:40216", ConnectionType: "inbound", Inbound: true,
		Network: "not_publicly_routable", Subver: "/ElectrumX:1.16.0/"}})[0]
	if got.App != "ElectrumX" || got.About != "Probably your own ElectrumX 1.16.0 on this Umbrel" {
		t.Errorf("electrumx: got %q / %q", got.About, got.App)
	}
	got = convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: ""}})[0]
	if got.Kind != noAgentKind || !strings.Contains(got.About, "checks the port") {
		t.Errorf("no agent: got %q / %q", got.Kind, got.About)
	}
}

// Inbound from umbrelOS's Docker gateway is IPv6 with the address hidden;
// outbound or any other private address is not.
func TestHiddenIPv6(t *testing.T) {
	cases := []struct {
		addr    string
		inbound bool
		want    string
	}{
		{"10.21.0.1:51234", true, hiddenIPv6},
		{"10.21.0.1:51234", false, "not_publicly_routable"},
		{"10.21.22.10:40000", true, torHidden},
		{"10.21.22.11:40000", true, i2pHidden},
		{"10.21.21.10:40216", true, "not_publicly_routable"}, // an app on this Umbrel
	}
	for _, c := range cases {
		typ := "outbound-full-relay"
		if c.inbound {
			typ = "inbound"
		}
		got := convert([]rawPeer{{Addr: c.addr, Network: "not_publicly_routable", ConnectionType: typ, Inbound: c.inbound, Subver: "/Satoshi:31.0.0/"}})
		if len(got) != 1 || got[0].Network != c.want {
			t.Errorf("%s inbound=%v: network %+v, want %s", c.addr, c.inbound, got, c.want)
		}
		wantOp := "Your Umbrel's own network"
		if c.want != "not_publicly_routable" {
			wantOp = ""
		}
		if got[0].Country != "" || got[0].Operator != wantOp {
			t.Errorf("%s: looked up %q %q", c.addr, got[0].Country, got[0].Operator)
		}
	}
}

func TestNamesSeenOnce(t *testing.T) {
	for subver, want := range map[string]string{
		"/dnsseedrs:0.2.0/":      "Crawler",
		"/ckh-netsurvey:1.0/":    "Crawler",
		"/bread:2.1/":            "Wallet",
		"//":                     noAgentKind,
		"/:1.1.0-dev+c1771608d/": noAgentKind,
		"/Satoshi:31.1.0/":       "Node (Core)",
	} {
		if got := kindOf(subver); got != want {
			t.Errorf("%s: got %q, want %q", subver, got, want)
		}
	}
	got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Version: 70016, Subver: "//", ServicesNames: []string{}}})[0]
	if !strings.HasPrefix(got.About, "Introduced itself without a name") {
		t.Errorf("// about: %q", got.About)
	}
}

func TestClientsFoundOn24October(t *testing.T) {
	for subver, want := range map[string]string{
		"/P2Pool:17.0/":    "Pool node",
		"/pydroid-pool:3/": "Pool node",
		"/Satoshi:29.4.2(mempool.guide)/Knots:20260508rc2/": "Node (Knots)",
		"/Ghost:1.11.42/":                        "Node (Ghost)",
		"/CamlCoin:0.1.0/":                       "Node (other)",
		"/rustoshi:0.1.0/":                       "Node (other)",
		"/bitcoin-rs:0.1.0/":                     "Node (other)",
		"/hotbuns:0.1.0/":                        "Node (other)",
		"/satd:0.2.1/":                           "Node (other)",
		"/bitcoinkernel-node:0.1.0/":             "Node (other)",
		"/tbcd:2.1.0-dev/":                       "Node (other)",
		"/mantle:0.1.0/":                         "Node (other)",
		"/mantle-replay-iouring:0.1.0/":          "Other",
		"/blueberry:2026.08.17/":                 "Wallet",
		"/BitcoinKit:0.1.0/":                     "Wallet",
		"Yuki / 0.0.1":                           "Light client",
		"/VerifiedX:9.0.1/":                      "Light client",
		"/WalletKit:0.1.0/":                      "Wallet",
		"/btcwire:0.4.1/Satoshi:0.19/":           "Research scanner",
		"/p2p-observer:0.1.0/":                   "Crawler",
		"/p2p-extractor:0.1.0/":                  "Crawler",
		"/bip324-probe:0.1.0/":                   "Crawler",
		"/crawlrs:0.2.0/":                        "Crawler",
		"/symphony:0.0.1/":                       "Indexer",
		"/your-fork:0.1/":                        "Other",
		"VDS 0.9.9.1":                            "Other chain",
		"/btcwire:0.5.0/CaribouSPV:0.0.1-beta/":  "Light client",
		"/btcwire:0.5.0/utreexod:0.5.0/":         "Node (other)",
		"NBXplorer-4096":                         "Indexer",
		"/bitcoin-s:1.9.12/":                     "Light client",
		"Stacks/2.1":                             "Light client",
		"/btcwire:0.4.1/lit:v0.1/":               "Lightning wallet",
		"/nakamoto:0.3.0/":                       "Light client",
		"/btcwire:0.5.0/spvwallet:0.1.0/":        "Wallet",
		"/bread:2.1/":                            "Wallet",
		"/bitcoinj:0.16.3/Elastum:0.1z/":         "Wallet",
		"/LionSeed:0.1.0/":                       "Crawler",
		"/gnoban:1.3.1/":                         "Crawler",
		"/Aurum:0.12.4/":                         "Other chain",
		"/bitcoinj:0.15.10.bisq.16/Bisq:1.10.9/": "Wallet",
	} {
		if got := kindOf(subver); got != want {
			t.Errorf("%s: got %q, want %q", subver, got, want)
		}
	}
	if got := aboutOf("/bitcoinj:0.15.10.bisq.16/Bisq:1.10.9/"); !strings.HasPrefix(got, "Bisq") {
		t.Errorf("Bisq about: %q", got)
	}
	for subver, want := range map[string]string{
		"/WalletKit:0.1.0/":              "Unstoppable Wallet on iPhone",
		"/btcwire:0.4.1/Satoshi:0.19/":   "Pool Detective",
		"/Denovo:0.7.0/":                 "Denovo",
		"/programmingbitcoin:0.1/":       "Code from Jimmy Song",
		"/programmingblockchain:0.1/":    "Code from Jimmy Song",
		"/bip324-ts:0.0.1/":              "bip324-ts",
		"/bread:2.1/":                    "BRD's wallet core",
		"/breadwallet:0.6.7/":            "BRD (breadwallet)",
		"/bitcoinj:0.16.3/Elastum:0.1z/": "Names Elastum",
	} {
		if got := aboutOf(subver); !strings.HasPrefix(got, want) {
			t.Errorf("%s about: %q", subver, got)
		}
	}
	if got := aboutOf("libbtc 0.1"); !strings.HasPrefix(got, "libbtc") {
		t.Errorf("libbtc about: %q", got)
	}
}

func TestLightningWalletReadsTheBareName(t *testing.T) {
	// A word in an operator's brackets does not make Core a Lightning wallet,
	// as with every kind in readsBareName; Bitcoin Lab reads it the same way.
	for subver, want := range map[string]string{
		"/Satoshi:28.0.0(neutrino)/": "Node (Core)",
		"/Satoshi:29.0.0(lit:v1)/":   "Node (Core)",
		"/neutrino:0.17.1/":          "Lightning wallet",
	} {
		if got := kindOf(subver); got != want {
			t.Errorf("%s: got %q, want %q", subver, got, want)
		}
	}
}

// The detail that opens on a tap needs what the peer announced.
func TestPeerCarriesServicesAndVersion(t *testing.T) {
	p := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:31.1.0/", Version: 70016,
		ServicesNames: []string{"NETWORK", "WITNESS", "NETWORK_LIMITED"}}})[0]
	if p.Version != 70016 || strings.Join(p.Services, ",") != "NETWORK,WITNESS,NETWORK_LIMITED" {
		t.Errorf("got version %d, services %v", p.Version, p.Services)
	}
}

func TestClientsFoundOn27October(t *testing.T) {
	if got := aboutOf("/bitcoin-rs:0.1.0/"); got == "" {
		t.Error("bitcoin-rs has no description")
	}
	if got, _ := classify("BTC RPC Proxy v0.8.1"); got != "Other" {
		t.Errorf("BTC RPC Proxy: kind %q, want Other", got)
	}
	if got := aboutOf("BTC RPC Proxy v0.8.1"); !strings.Contains(got, "RPC proxy") {
		t.Errorf("BTC RPC Proxy: about %q", got)
	}
}

// neutrino's version tells the lnd generation; any other stays general.
func TestNeutrinoVersion(t *testing.T) {
	for subver, want := range map[string]string{
		"/btcwire:0.5.0/neutrino:0.17.1/":          "lnd 0.21 or newer, without a node of its own",
		"/btcwire:0.5.0/neutrino:0.12.0-beta/":     "Older lnd, without a node of its own",
		"/btcwire:0.5.0/neutrino:0.11.0-beta/":     "Neutrino: Lightning wallets on lnd",
		"/btcwire:0.5.0/neutrino:0.12.0-beta-zap/": "Neutrino: Lightning wallets on lnd",
	} {
		if got := aboutOf(subver); !strings.HasPrefix(got, want) {
			t.Errorf("%s: %q, want %q", subver, got, want)
		}
	}
}

// libbitcoin 4 speaks BIP324: offering P2P_V2 is no sign of a disguise there,
// before 4 it still is.
func TestLibbitcoinV2(t *testing.T) {
	v2 := []string{"NETWORK", "WITNESS", "P2P_V2"}
	if got := anachronism("/Libbitcoin:4.0.0/", v2); got != "" {
		t.Errorf("4.0.0: %q", got)
	}
	if got := anachronism("/libbitcoin:3.8.0/", v2); got == "" {
		t.Error("3.8.0 with P2P_V2 not flagged")
	}
}

// Bitcoin Cash clients carry "(EB8.0)" like Classic but are not Classic, and
// every other chain, and the names without a public source, still get a sentence.
func TestOtherChainAbouts(t *testing.T) {
	for subver, want := range map[string]string{
		"/Bitcoin ABC:0.16.1(EB8.0)/": "Bitcoin ABC",
		"/BUCash:1.9.0(EB32; AD12)/":  "Bitcoin Unlimited",
		"/BCHUnlimited:1.0/":          "Bitcoin Unlimited",
		"/Bitcoin SV:1.0.16/":         "Bitcoin SV",
		"/Bitcoin XT:0.11.0/":         "Bitcoin XT",
		"/Classic:1.3.8(EB8)/":        "Bitcoin Classic",
		"/SuperBitcoin:0.16.0.2/":     "Super Bitcoin",
		"vds_0.9.53":                  "VDS",
		"/iriumd:1.0/":                "Irium",
		"/cf-package-relay:0.3.0/":    "Named after package relay",
	} {
		if got := aboutOf(subver); !strings.HasPrefix(got, want) {
			t.Errorf("%s: about %q, want %q", subver, got, want)
		}
	}
}

// Names with no public source are not given a kind from what one node saw
// them do, nor a fixed sentence: the session decides what is said. A name
// that says what it is does get the kind it names.
func TestNoKindFromOneNode(t *testing.T) {
	for _, ua := range []string{"/trinetra:0.1/", "/your-fork:0.1/", "/cryptexplorer:1.0/", "/BlockSail:1.0/", "/multiseed:0.1/", "/Prometheus:30.4.1/", "/Natasha:1.0/"} {
		if got := kindOf(ua); got != "Other" {
			t.Errorf("%s: kind %q, want Other", ua, got)
		}
		if got := aboutOf(ua); got != "" {
			t.Errorf("%s: fixed sentence %q", ua, got)
		}
	}
}

func TestKindFromWhatTheNameSays(t *testing.T) {
	for ua, want := range map[string]string{
		"/topology-probe:0.1/": "Crawler", "/p2p-probe:0.1.0/": "Crawler", "/probe:1.0/": "Crawler",
		"/btc-peers:0.1/": "Crawler", "/alt-bitnodes:0.1/": "Crawler",
		"/bip157-fetch:0.1/": "Light client", "/dexcash-nodes:0.1.0/": "Other chain",
	} {
		if got := kindOf(ua); got != want {
			t.Errorf("%s: kind %q, want %q", ua, got, want)
		}
	}
}

// Named from their own code or 0xB10C's list; all three were "Other" before.
func TestScannersFromCodeAndList(t *testing.T) {
	for _, c := range []struct{ subver, kind, about string }{
		{"/connector/", "Research scanner", "DaeConn"},
		{"/nodes.mom.market:0.2/", "Crawler", "nodes.mom.market"},
		{"/btcwire:0.5.0/bitcoinstats.com:0.2/", "Crawler", "bitcoinstats.com"},
		{"/bitcoin-peers:0.2.0/", "Crawler", "bitcoin-peers"},
		{"/logos-peer-list:0.1.0/", "Crawler", "Logos peer list"},
		{"/ouroboros:0.1.0/", "Node (other)", "ouroboros"},
		{"/mako:0.0.0/", "Node (other)", "mako"},
		{"/OpenBitcoin-crawler:0.1/", "Crawler", "OpenBitcoin's crawler"},
		{"/NBitcoin:9.0.0/", "Other", "NBitcoin"},
		// Similar names are not evidence for a match.
		{"/connector:1.0/", "Other", ""},
		{"/Satoshi:31.1.0(bitcoinstats.com)/", "Node (Core)", ""},
	} {
		if got := kindOf(c.subver); got != c.kind {
			t.Errorf("%s: kind %q, want %q", c.subver, got, c.kind)
		}
		if got := aboutOf(c.subver); !strings.Contains(got, c.about) || c.about == "" && got != "" {
			t.Errorf("%s: about %q, want %q", c.subver, got, c.about)
		}
	}
}

func TestClientsCheckedOn9November(t *testing.T) {
	for _, c := range []struct {
		subver, kind, about string
	}{
		{"/GrayersDad:08.30.2026BLAKE2b/", "Node (Knots)", "Probably Bitcoin Knots"},
		{"/GrayersDad:08.08.2026RDTS-in-Spirit (Node 1)/", "Node (Knots)", "Probably Bitcoin Knots"},
		{"/BTC-Nodes:2026-10-31/Probe/", "Crawler", "Named like BTC Nodes"},
		{"/BTC-Nodes:2026-07-14/Sonar/", "Crawler", "Named like BTC Nodes"},
		{"/BTC-Nodes:2026-10-31/", "Other", "Named like BTC Nodes"},
		// The code of these sets other names, so they stay unknown.
		{"/chaingun:0.4/", "Other", ""},
		{"/spynode:0.1/", "Other", ""},
	} {
		if got := kindOf(c.subver); got != c.kind {
			t.Errorf("%s: kind %q, want %q", c.subver, got, c.kind)
		}
		if got := aboutOf(c.subver); !strings.Contains(got, c.about) || c.about == "" && got != "" {
			t.Errorf("%s: about %q, want %q", c.subver, got, c.about)
		}
	}
}
