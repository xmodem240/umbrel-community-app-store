package main

import (
	"strings"
	"testing"
	"time"
)

func withBCH(t *testing.T) {
	t.Helper()
	bchMode = true
	t.Cleanup(func() { bchMode = false })
}

func TestBitcoinCashClientsAreNodes(t *testing.T) {
	withBCH(t)
	for subver, want := range map[string]string{
		"/Bitcoin Cash Node:29.0.0(EB32.0)/": "Node (BCHN)",
		"/bchd:0.21.1(EB32.0)/":              "Node (bchd)",
		"/BCH Unlimited:2.1.0(EB32; AD12)/":  "Node (BCH Unlimited)",
		"/Flowee:1(2024-07.1)/":              "Node (Flowee)",
		"/Knuth:0.58.0/":                     "Node (Knuth)",
		"/Bitcoin Verde:2.2.0/":              "Node (Bitcoin Verde)",
		"/SomethingNew:1.0(EB32.0)/":         "Node (other)",
		"/Satoshi:29.0.0/":                   "Other chain",
		"/Satoshi:29.1.0/Knots:20250903/":    "Other chain",
		"/Bitcoin ABC:0.30.5(EB32.0)/":       "Other chain",
		"/Bitcoin SV:1.0.16/":                "Other chain",
		"/bitcoin-seeder:0.01/":              "Crawler",
		"":                                   noAgentKind,
	} {
		got, relays := classify(subver)
		if got != want {
			t.Errorf("classify(%q) = %q, want %q", subver, got, want)
		}
		if isNode := strings.HasPrefix(want, "Node ("); (isNode && !relays) || (want == "Other chain" && relays) {
			t.Errorf("classify(%q) relays = %v", subver, relays)
		}
	}
}

// A Bitcoin Cash node is never held to Bitcoin Core's release history, and
// Bitcoin software on this network is another chain, not a disguise.
func TestBitcoinCashPeersAreNotDisguised(t *testing.T) {
	withBCH(t)
	now := time.Unix(1_800_000_000, 0)
	yes := true
	h := int64(900000)
	raw := []rawPeer{
		{ID: 1, Addr: "8.8.8.8:8333", Network: "ipv4", ConnectionType: "outbound-full-relay", Subver: "/Bitcoin Cash Node:29.0.0(EB32.0)/",
			Version: 70016, ServicesNames: []string{"NETWORK", "BLOOM", "BITCOIN_CASH", "UNKNOWN[2^27]"}, RelayTxes: &yes, SyncedHeaders: &h, ConnTime: now.Unix() - 3600},
		{ID: 2, Addr: "1.1.1.1:8333", Network: "ipv4", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:0.16.1/",
			Version: 70016, ServicesNames: []string{"NETWORK", "WITNESS", "P2P_V2"}, RelayTxes: &yes, SyncedHeaders: &h, ConnTime: now.Unix() - 3600},
	}
	got := convertAt(raw, now)
	if len(got) != 2 {
		t.Fatalf("got %d peers", len(got))
	}
	if got[0].Kind != "Node (BCHN)" || got[0].NoRelay || got[0].Fork != "" || got[0].Label != "Bitcoin Cash Node" {
		t.Errorf("BCHN peer: %+v", got[0])
	}
	if got[1].Kind != "Other chain" || !got[1].NoRelay || got[1].Tell != "" {
		t.Errorf("Bitcoin peer: kind %q no_relay %v tell %q about %q", got[1].Kind, got[1].NoRelay, got[1].Tell, got[1].About)
	}
}
