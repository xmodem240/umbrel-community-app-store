package main

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Every reason a peer is called disguised has its few words for the list:
// the demo data has a peer for each rule, and each one must say why under
// its pill.
func TestEveryDisguiseHasATell(t *testing.T) {
	raw, err := mockPeers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, p := range convertAt(raw, time.Now()) {
		if p.Kind != fakeWalletKind {
			if p.Tell != "" && p.Kind != "Other" && p.Kind != noAgentKind {
				t.Errorf("%s is %s but tells %q", p.Subver, p.Kind, p.Tell)
			}
			continue
		}
		seen++
		if p.Tell == "" {
			t.Errorf("%s: no short reason for %q", p.Subver, p.About)
		}
	}
	if seen == 0 {
		t.Fatal("the demo data has no disguised peer")
	}
}

func TestShortReasons(t *testing.T) {
	for _, c := range []struct{ about, want string }{
		{"Claims: Bitcoin Core 25.1.0\nProbably: " + skippedHandshakeAbout(map[string]int64{"sendaddrv2": 1}), "no wtxidrelay"},
		{"Claims: Bitcoin Core 28.1.0\nProbably: " + offersNothingAbout + "\nAlso: it left out wtxidrelay", "offers no services \u00b7 no wtxidrelay"},
		// What it did first, whatever order the reasons came in.
		{fakeAbout("/Sat0shi:25.0.0/", "", nil) + "\nAlso: not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes", "pings every 2 s \u00b7 misspelt name"},
		{"Claims: x\nProbably: " + offersNothingAbout + "\nAlso: it left out wtxidrelay\nAlso: a crawler - it asked for addresses 18 times, Core asks once", "asks for addresses 18\u00d7 \u00b7 offers no services"},
		{"Claims: Bitcoin Core 31.1.0\nProbably: " + anachronism("/Satoshi:31.1.0/", []string{"NETWORK", "WITNESS"}), "no NETWORK_LIMITED"},
		{"Claims: Bitcoin Core 25.1.0\nProbably: " + protocolMismatch("/Satoshi:25.1.0/", 70015), "protocol 70015 for Core 25.1.0"},
		{"Claims: x\nProbably: a crawler - it asked for addresses 18 times, Core asks once", "asks for addresses 18×"},
		{"Claims: x\nProbably: not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes", "pings every 2 s"},
		{"Claims: x\nProbably: " + bitcoinjTooNewAbout, "newer protocol than the wallet"},
		// A protocol number only where nothing else tells.
		{"Claims: Bitcoin Core 25.1.0\nProbably: " + protocolMismatch("/Satoshi:25.1.0/", 70015) + "\nAlso: not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes", "pings every 2 s"},
		{"Claims: Bitcoin Core 23.0.0\nProbably: " + protocolMismatch("/Satoshi:23.0.0/", 70002) + "\nAlso: " + offersNothingAbout + "\nAlso: it left out wtxidrelay", "offers no services \u00b7 no wtxidrelay"},
		{"Claims: x\nProbably: " + anachronism("/Satoshi:0.15.0/Knots:20170914/", []string{"NETWORK", "P2P_V2"}), "v2 for Core 0.15"},
		{fakeAbout("/Satoshi:0.11.0/", "", nil) + "\nAlso: " + protocolMismatch("/Satoshi:0.11.0/", 70016), "protocol 70016 for Core 0.11.0"},
		{fakeAbout("/Sat0shi:25.0.0/", "", nil), "misspelt name"},
		{fakeAbout("Satoshi:25.0.0", "", nil), "misspelt name"},
		{fakeAbout("/Satoshi9:25.0.0/", "", nil), "misspelt name"},
	} {
		if got := tellOf(c.about, false); got != c.want {
			t.Errorf("tellOf(%q) = %q, want %q", c.about, got, c.want)
		}
	}
	lion := "Claims: bitcoinj 0.14.5\nProbably: " + linkingLionAbout + "\nAlso: it offers blocks, which no wallet does"
	if got := tellOf(lion, true); got != "wallet offering blocks" {
		t.Errorf("LinkingLion with blocks tells %q", got)
	}
	// The wide list's sentence is the reason the short line leads with.
	ping := "Claims: Bitcoin Core 25.1.0\nProbably: " + protocolMismatch("/Satoshi:25.1.0/", 70015) + "\nAlso: not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes"
	if _, why := tellAndWhy(ping, false); why != "Probably: not Bitcoin Core - it pings every 2 seconds, Core every 2 minutes" {
		t.Errorf("why leads with %q", why)
	}
	if _, why := tellAndWhy("Claims: Bitcoin Core 0.11.0\nProbably: "+linkingLionAbout, true); why != "" {
		t.Errorf("bare LinkingLion has why %q", why)
	}
	// One piece of evidence: the pill already says what it is.
	if got := tellOf(lion+"\nAlso: it speaks v2, which no wallet does", true); got != "wallet offering blocks" {
		t.Errorf("LinkingLion with two reasons tells %q", got)
	}
	if got := tellOf("Claims: Bitcoin Core 0.11.0\nProbably: "+linkingLionAbout, true); got != "on 0xB10C's list" {
		t.Errorf("bare LinkingLion tells %q", got)
	}
}

// Every sentence otherAbout and noAgentAbout can write has its few words,
// except the two that say nothing was seen. Read from the source, so a new
// sentence without a short form fails here.
func TestEveryBehaviourHasATell(t *testing.T) {
	src, err := os.ReadFile("about.go")
	if err != nil {
		t.Fatal(err)
	}
	// No short form where nothing was seen, and where real nodes do the
	// same (see otherAbout): there the line keeps what Core saw.
	// The sentences with two readings are built from parts and not read
	// here; TestBlockFollowerHasNoTell and TestTwoReadings cover them.
	quiet := map[string]bool{
		"Software with a name we don't recognise":                           true,
		"Introduced itself without a name; nothing it did tells what it is": true,
		"Asks for headers and addresses and wants no transactions: ":        true,
		"Asks for headers and wants no transactions: ":                      true,
		"Fetches transactions from your node and announces none: ":          true,
		"Asks for addresses, not for headers or blocks: ":                   true,
		"Asks for addresses, not for headers or blocks":                     true,
	}
	n := 0
	for _, fn := range []string{"func sessionLine(", "func noAgentLine("} {
		body := string(src[strings.Index(string(src), fn):])
		body = body[:strings.Index(body, "\n}\n")]
		for _, m := range regexp.MustCompile(`return "([^"]+)"`).FindAllStringSubmatch(body, -1) {
			n++
			got := behaviourTell(m[1])
			if quiet[m[1]] != (got == "") {
				t.Errorf("%q tells %q", m[1], got)
			}
		}
	}
	if n < 10 {
		t.Fatalf("found only %d sentences - did the functions move?", n)
	}
}

// Headers and no transactions is what a real node's block-only connection
// does too: the list says nothing about it, the panel gives both readings.
func TestBlockFollowerHasNoTell(t *testing.T) {
	no := false
	for _, recv := range []map[string]int64{{"getheaders": 1}, {"getheaders": 1, "getaddr": 1}, {"sendheaders": 1}} {
		about := otherAbout(nil, &no, recv, map[string]int64{}, time.Now().Unix()-60, time.Now())
		if got := behaviourTell(about); got != "" {
			t.Errorf("%v: %q tells %q", recv, about, got)
		}
	}
	if got := behaviourTell(noAgentAbout(map[string]int64{"getheaders": 1, "getaddr": 1}, 0, time.Now())); got != "headers + addresses" {
		t.Errorf("a nameless node tells %q", got)
	}
	if got := behaviourTell(noAgentAbout(map[string]int64{"getheaders": 1, "sendcmpct": 1}, 0, time.Now())); got != "headers + compact blocks" {
		t.Errorf("a nameless node with compact blocks tells %q", got)
	}
}

// A peer offering no service cannot be the node reading: a real node always
// offers one.
func TestTwoReadings(t *testing.T) {
	no := false
	recv := map[string]int64{"getheaders": 1}
	if got := otherAbout([]string{"WITNESS"}, &no, recv, nil, time.Now().Unix()-60, time.Now()); got != "Asks for headers and wants no transactions: a block-only connection, or a program that tracks the chain" {
		t.Errorf("with services: %q", got)
	}
	if got := otherAbout([]string{}, &no, recv, nil, time.Now().Unix()-60, time.Now()); got != "Asks for headers and wants no transactions: a program that tracks the chain" {
		t.Errorf("without services: %q", got)
	}
	if got := otherAbout([]string{}, &no, map[string]int64{"getaddr": 2}, nil, time.Now().Unix()-60, time.Now()); got != "Asks for addresses, not for headers or blocks: a crawler" {
		t.Errorf("addresses without services: %q", got)
	}
}
