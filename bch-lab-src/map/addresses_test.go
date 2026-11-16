package main

import (
	"strings"
	"testing"
)

func TestLinkingLion(t *testing.T) {
	for _, a := range []string{"143.20.137.5:51234", "[2602:f5c0:0:ace::72:300]:8333", "[2602:f5c0:0:ace::60]:8333",
		"104.234.118.2:41000", "69.17.52.1:8333", "91.198.115.9:1", "[::ffff:31.58.215.200]:8333", "199.116.84.20:8333"} {
		if !isLinkingLion(a) {
			t.Errorf("%s not recognised", a)
		}
	}
	// the rest of the provider's /32 and the neighbours of the single hosts are not LinkingLion
	for _, a := range []string{"143.20.138.5:8333", "[2604:d500:4:2::1]:8333", "[2602:f5c0:1::7]:8333",
		"104.234.118.3:8333", "69.17.52.2:8333", "10.21.0.4:40000", "abc.onion:8333", ""} {
		if isLinkingLion(a) {
			t.Errorf("%s wrongly recognised", a)
		}
	}
	got := convert([]rawPeer{
		{Addr: "87.229.79.10:40000", Subver: "/bitcoinj:0.14.4/Bitcoin Wallet:5.24/", ServicesNames: []string{"NETWORK", "P2P_V2"}, Inbound: true},
		{Addr: "87.229.79.11:40000", Subver: "", Inbound: true},
	})
	if len(got) != 2 {
		t.Fatalf("got %d peers, want 2", len(got))
	}
	// with a borrowed name: Disguised; without any name: still "No user agent"
	if got[0].Kind != fakeWalletKind || !strings.Contains(got[0].About, linkingLionAbout) {
		t.Errorf("%s: kind %q about %q", got[0].Addr, got[0].Kind, got[0].About)
	}
	if got[1].Kind != noAgentKind || !strings.Contains(got[1].About, linkingLionAbout) {
		t.Errorf("%s: kind %q about %q", got[1].Addr, got[1].Kind, got[1].About)
	}
}

// Every LinkingLion peer is flagged for the header count, whatever kind its
// user agent made it; nothing else is.
func TestLinkingLionFlag(t *testing.T) {
	got := convert([]rawPeer{
		{Addr: "104.234.118.2:41000", Inbound: true, ConnectionType: "inbound", Subver: "/bitcoinj:0.14.4/Bitcoin Wallet:5.36/"},
		{Addr: "143.20.137.9:41000", Inbound: true, ConnectionType: "inbound", Subver: ""},
		{Addr: "198.51.100.7:8333", Inbound: true, ConnectionType: "inbound", Subver: "/Satoshi:31.0.0/", ServicesNames: []string{"NETWORK", "WITNESS"}},
	})
	if !got[0].LinkingLion || !got[1].LinkingLion || got[2].LinkingLion {
		t.Fatalf("flags: %v %v %v", got[0].LinkingLion, got[1].LinkingLion, got[2].LinkingLion)
	}
}

// LinkingLion's verdict comes from its address; what it did itself stands
// under it, so the disguise would show without the list too.
func TestLinkingLionShowsItsOwnEvidence(t *testing.T) {
	got := convert([]rawPeer{{Addr: "143.20.137.5:8333", ConnectionType: "inbound", Inbound: true,
		Subver: "/Satoshi:0.12.1/", Version: 70016, ServicesNames: []string{"NETWORK"}}})[0]
	if !got.LinkingLion || !strings.Contains(got.About, "Probably: LinkingLion") || !strings.Contains(got.About, "\nAlso: ") {
		t.Errorf("%q", got.About)
	}
}

// A university scanner under a wallet's name is named by its address.
func TestResearchHosts(t *testing.T) {
	got := convert([]rawPeer{
		{ID: 1, Addr: "147.229.8.240:49422", ConnectionType: "inbound", Inbound: true, Subver: "/bitcoinj:0.16.5/Bitcoin Wallet:10.26/", Version: 70017, ServicesNames: []string{}},
		{ID: 2, Addr: "147.229.8.241:49422", ConnectionType: "inbound", Inbound: true, Subver: "/bitcoinj:0.16.5/Bitcoin Wallet:10.26/", Version: 70017, ServicesNames: []string{}},
	})
	if got[0].Kind != "Research scanner" || !strings.Contains(got[0].About, "Brno") || !strings.Contains(got[0].PoolProof, "anton1.fit.vutbr.cz") {
		t.Fatalf("Brno: %q %q %q", got[0].Kind, got[0].About, got[0].PoolProof)
	}
	if got[1].Kind == "Research scanner" {
		t.Fatalf("neighbour: %q", got[1].Kind)
	}
}
