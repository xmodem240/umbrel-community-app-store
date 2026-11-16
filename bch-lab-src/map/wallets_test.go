package main

import (
	"strings"
	"testing"
	"time"
)

// A 2016 wallet name on something offering blocks and v2 is not that wallet.
// The real Bitcoin Wallet, offering nothing, stays a wallet and says what it is.
func TestFakeWallet(t *testing.T) {
	cases := []struct {
		subver    string
		services  []string
		kind      string
		aboutSays string
	}{
		{"/bitcoinj:0.14.5/Bitcoin Wallet:5.42/", []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}, fakeWalletKind, "Probably: a node or scanner behind an old wallet name"},
		{"/breadwallet:0.6.5/", []string{"NETWORK"}, fakeWalletKind, "Probably: a node or scanner behind an old wallet name"},
		{"/bitcoinj:0.16.2/Bitcoin Wallet:9.26/", []string{}, "Wallet", "Schildbach"},
		{"/bitcoinj:0.16.2/Bitcoin Wallet:9.26/", nil, "Wallet", "Schildbach"},
		// Core's name misspelt: what it claims, and a guess from what it offers.
		{"/Sat0shi:31.0.0/", []string{}, fakeWalletKind, "Claims: Bitcoin Core 31.0.0\nProbably: not Bitcoin Core - it writes Sat0shi with a zero and offers no services"},
		{"/Satoshi2:0.18.3/", []string{"NETWORK", "WITNESS"}, fakeWalletKind, "Probably: not Bitcoin Core - Core calls itself /Satoshi:, this one /Satoshi2:"},
		{"/SatoshiX:0.18.0/", []string{"NETWORK", "WITNESS"}, fakeWalletKind, "Probably: not Bitcoin Core - Core calls itself /Satoshi:, this one /SatoshiX:"},
		{"Satoshi:22.0.0", []string{"NETWORK"}, fakeWalletKind, "Claims: Bitcoin Core 22.0.0\nProbably: not Bitcoin Core - Core writes its name between slashes: /Satoshi:22.0.0/"},
		// Only a wallet name can be fake: a node offering blocks is a node.
		{"/Satoshi:31.1.0/", []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}, "Node (Core)", ""},
	}
	for _, c := range cases {
		got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
			Subver: c.subver, ServicesNames: c.services}})[0]
		if got.Kind != c.kind {
			t.Errorf("%s %v: kind %q, want %q", c.subver, c.services, got.Kind, c.kind)
		}
		if c.aboutSays == "" && got.About != "" || !strings.Contains(got.About, c.aboutSays) {
			t.Errorf("%s %v: about %q, want it to say %q", c.subver, c.services, got.About, c.aboutSays)
		}
	}
}

// bitcoinj has never sent more than 70013, so a wallet name on 70016 is not
// that wallet - even when it offers nothing, like the real one.
func TestBitcoinjTooNew(t *testing.T) {
	cases := []struct {
		subver  string
		version int
		kind    string
		about   string
	}{
		{"/bitcoinj:0.16.2/Bitcoin Wallet:9.26/", 70016, fakeWalletKind, "Claims: the wallet Bitcoin Wallet by Schildbach\nProbably: not a wallet - speaks a newer protocol than this wallet ever did"},
		{"/bitcoinj:0.16.5/Bitcoin Wallet:10.26/", 70015, fakeWalletKind, "Probably: not a wallet"},
		// Another program on bitcoinj could set its own number, a later
		// bitcoinj could raise it: both are left alone.
		{"/bitcoinj:0.16/", 70016, "Wallet", ""},
		{"/bitcoinj:0.18.0/Bitcoin Wallet:12.0/", 70016, "Wallet", "Schildbach"},
		{"/bitcoinj:0.16.2/Bitcoin Wallet:9.26/", 70013, "Wallet", "Schildbach"},
		{"/bitcoinj:0.14.6/", 70001, "Wallet", ""},
		// A version Core did not report says nothing either way.
		{"/bitcoinj:0.16.5/Bitcoin Wallet:10.26/", 0, "Wallet", "Schildbach"},
		// Neutrino sends 70013 or 70016 depending on its btcd: left alone.
		{"/btcwire:0.5.0/neutrino:0.17.1/", 70016, "Lightning wallet", "lnd 0.21"},
	}
	for _, c := range cases {
		got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
			Subver: c.subver, Version: c.version, ServicesNames: []string{}}})[0]
		if got.Kind != c.kind || !strings.Contains(got.About, c.about) {
			t.Errorf("%s %d: %q / %q, want %q / %q", c.subver, c.version, got.Kind, got.About, c.kind, c.about)
		}
	}
}

// The time connected is added once it is a day or more, never on its own.
func TestStayedLong(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := []struct {
		hours float64
		want  string
	}{
		{1.4, ""},
		{23.9, ""},
		{24, ", and has stayed connected for a day"},
		{278, ", and has stayed connected for 11 days"},
	}
	for _, c := range cases {
		start := now.Add(-time.Duration(c.hours * float64(time.Hour))).Unix()
		if got := stayedLong(start, now); got != c.want {
			t.Errorf("%.1f h: %q, want %q", c.hours, got, c.want)
		}
	}
	if got := stayedLong(0, now); got != "" {
		t.Errorf("no conntime: %q", got)
	}
}

// bitcoinj without an app name, offering blocks, claims bitcoinj - not Core.
func TestPlainBitcoinjOfferingBlocks(t *testing.T) {
	got := convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
		Subver: "/bitcoinj:0.14.5/", Version: 70016, ServicesNames: []string{"NETWORK", "P2P_V2"}}})[0]
	want := "Claims: bitcoinj 0.14.5\nProbably: a node or scanner behind an old wallet name - it offers blocks, which no wallet does" +
		"\nAlso: it offers encrypted v2 connections, which that software never had"
	if got.Kind != "Disguised" || got.About != want {
		t.Errorf("kind %q about %q", got.Kind, got.About)
	}
}

func TestWasabiIsAWallet(t *testing.T) {
	conn := time.Now().Unix() - 3600
	yes, no := true, false
	wasabi := rawPeer{Addr: "185.220.101.1:40000", ConnectionType: "inbound", Inbound: true, Version: 70016,
		Subver: "/Satoshi:29.3.0/", Transport: "v1", ConnTime: conn, ServicesNames: []string{}, RelayTxes: &yes,
		BytesRecvPerMsg: map[string]int64{"ping": 32 * 30, "getheaders": 1053 * 15, "getdata": 300000}}
	got := convert([]rawPeer{wasabi})[0]
	if got.Kind != "Wallet" || got.About != "Claims: Bitcoin Core 29.3.0\nProbably: Wasabi Wallet or a fork - one of its 13 borrowed Core names, offers nothing, wants transactions" || !got.NoRelay {
		t.Errorf("Wasabi: got %q / %q / norelay %v", got.Kind, got.About, got.NoRelay)
	}
	// Without transaction relay it is no wallet.
	r := wasabi
	r.RelayTxes = &no
	if got := convert([]rawPeer{r})[0]; got.Kind != fakeWalletKind {
		t.Errorf("no tx relay: got %q", got.Kind)
	}
	// A name outside Wasabi's list stays Disguised.
	r = wasabi
	r.Subver = "/Satoshi:31.0.0/"
	if got := convert([]rawPeer{r})[0]; got.Kind != fakeWalletKind {
		t.Errorf("31.0.0: got %q", got.Kind)
	}
	// Offering services, it is a node.
	r = wasabi
	r.ServicesNames = []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}
	if got := convert([]rawPeer{r})[0]; got.Kind != "Node (Core)" {
		t.Errorf("with services: got %q", got.Kind)
	}
	// Ginger's older list, and the one name on both.
	r = wasabi
	r.Subver = "/Satoshi:0.21.0/"
	if got := convert([]rawPeer{r})[0]; got.Kind != "Wallet" || got.About != "Claims: Bitcoin Core 0.21.0\nProbably: Ginger Wallet (or an old Wasabi) - one of its 31 borrowed Core names, offers nothing, wants transactions" {
		t.Errorf("Ginger: got %q / %q", got.Kind, got.About)
	}
	r.Subver = "/Satoshi:26.0.0/"
	if got := convert([]rawPeer{r})[0]; got.About != "Claims: Bitcoin Core 26.0.0\nProbably: Wasabi or Ginger Wallet - a borrowed Core name both use, offers nothing, wants transactions" {
		t.Errorf("26.0.0: got %q", got.About)
	}
	// Asking for addresses again and again is a crawler, whatever the name.
	r = wasabi
	r.BytesRecvPerMsg = map[string]int64{"getaddr": 24 * 20}
	if got := convert([]rawPeer{r})[0]; got.Kind != fakeWalletKind {
		t.Errorf("getaddr crawler: got %q", got.Kind)
	}
}

func TestWasabiPeerSearch(t *testing.T) {
	no, yes := false, true
	search := rawPeer{Addr: "185.220.101.1:40000", ConnectionType: "inbound", Inbound: true, Version: 70016,
		Subver: "/Satoshi:27.0.0/", Transport: "v1", ConnTime: time.Now().Unix() - 5, ServicesNames: []string{}, RelayTxes: &no,
		BytesRecvPerMsg: map[string]int64{"version": 126, "sendaddrv2": 24, "verack": 24, "getaddr": 24}}
	got := convert([]rawPeer{search})[0]
	if got.Kind != "Crawler" || got.About != "Claims: Bitcoin Core 27.0.0\nProbably: Wasabi Wallet's peer search - one of its 13 borrowed Core names, offers nothing, asks only for addresses" {
		t.Errorf("peer search: got %q / %q", got.Kind, got.About)
	}
	for name, change := range map[string]func(r *rawPeer){
		"sendcmpct instead of sendaddrv2": func(r *rawPeer) {
			r.BytesRecvPerMsg = map[string]int64{"version": 126, "sendcmpct": 33, "verack": 24, "getaddr": 24}
		},
		"Ginger's name only": func(r *rawPeer) { r.Subver = "/Satoshi:0.21.0/" },
		"protocol 70015":     func(r *rawPeer) { r.Version = 70015 },
	} {
		r := search
		change(&r)
		if got := convert([]rawPeer{r})[0]; got.Kind == "Crawler" || strings.Contains(got.About, "Wasabi") {
			t.Errorf("%s: got %q / %q", name, got.Kind, got.About)
		}
	}
	// A wallet that sends wtxidrelay is not on NBitcoin.
	r := search
	r.RelayTxes = &yes
	r.BytesRecvPerMsg = map[string]int64{"version": 126, "wtxidrelay": 24, "sendaddrv2": 24, "verack": 24}
	if got := convert([]rawPeer{r})[0]; got.Kind == "Wallet" {
		t.Errorf("wtxidrelay wallet: got %q / %q", got.Kind, got.About)
	}
}

func TestVirtusCrawlerBehindCore23(t *testing.T) {
	no := false
	r := rawPeer{Addr: "198.51.100.7:50000", ConnectionType: "inbound", Inbound: true, Version: 70015,
		Subver: "/Satoshi:23.0.0/", ServicesNames: []string{}, RelayTxes: &no,
		BytesRecvPerMsg: map[string]int64{"version": 126, "verack": 24}}
	got := convert([]rawPeer{r})[0]
	if got.Kind != "Crawler" || !strings.Contains(got.About, "p2p-crawler") {
		t.Errorf("virtu's crawler: got %q / %q", got.Kind, got.About)
	}
	r.Version = 70016
	if got := convert([]rawPeer{r})[0]; strings.Contains(got.About, "p2p-crawler") {
		t.Errorf("70016 is not the crawler: %q", got.About)
	}
}

// The second reason next to the protocol is what the peer never asked for -
// but only once it has been connected long enough for a real wallet to have.
func TestBitcoinjAskedNothing(t *testing.T) {
	now := time.Now()
	wallet := func(connAgo int64, recv map[string]int64) string {
		return convert([]rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true,
			Subver: "/bitcoinj:0.16.5/Bitcoin Wallet:10.26/", Version: 70017, ServicesNames: []string{},
			ConnTime: now.Unix() - connAgo, BytesRecvPerMsg: recv}})[0].About
	}
	quiet := map[string]int64{"version": 124, "verack": 24, "ping": 32, "pong": 32, "getaddr": 24}
	if got := wallet(3*86400, quiet); !strings.HasSuffix(got, "never asked for a header or a transaction") {
		t.Errorf("quiet for 3 days: %q", got)
	}
	// Just connected: a real wallet has not had the time to ask either way.
	if got := wallet(30, quiet); strings.Contains(got, "never asked") {
		t.Errorf("30 seconds: %q", got)
	}
	// It asked for headers: the clause is not true, the duration is said instead.
	asked := map[string]int64{"version": 124, "getheaders": 1061}
	if got := wallet(3*86400, asked); strings.Contains(got, "never asked") || !strings.Contains(got, "3 days") {
		t.Errorf("asked for headers: %q", got)
	}
	// A node that did not report the counters says nothing about it.
	if got := wallet(3*86400, nil); strings.Contains(got, "never asked") {
		t.Errorf("no counters: %q", got)
	}
}
