package main

import "regexp"

// Bitcoin Cash Lab's build of this app.
//
// The rules in kinds.go, disguise.go, forks.go and pools.go were written for
// Bitcoin: they call a Bitcoin Cash client "Other chain", hold a peer to what
// Bitcoin Core's releases do, read Knots' service bits and know the addresses
// of Bitcoin pools' nodes. On a Bitcoin Cash node all of that is upside down,
// so with bchMode on:
//
//   - a Bitcoin Cash node by its own name is a node, and is never "Disguised";
//   - Bitcoin software (/Satoshi:, Knots) and the other forks are the other chain;
//   - the Bitcoin-only fork bits and pool addresses are not read at all.
//
// Everything about where a peer is, who hosts it and what it did in this
// session is the same on both chains and stays as it is. Off in the tests of
// the files above, which keep testing what they were written for.
var bchMode bool

var bchRules = []struct {
	name   string
	re     *regexp.Regexp
	relays bool
}{
	// Bitcoin Cash Node says "/Bitcoin Cash Node:29.0.0(EB32.0)/".
	{"Node (BCHN)", regexp.MustCompile(`^/Bitcoin Cash Node:`), true},
	{"Node (bchd)", regexp.MustCompile(`(?i)^/bchd:`), true},
	// "/BCH Unlimited:2.1.0(EB32; AD12)/", formerly BUCash.
	{"Node (BCH Unlimited)", regexp.MustCompile(`(?i)^/BCH ?Unlimited:|^/BUCash:`), true},
	{"Node (Flowee)", regexp.MustCompile(`(?i)^/Flowee`), true},
	{"Node (Knuth)", regexp.MustCompile(`(?i)^/Knuth:|^/kth[:-]`), true},
	{"Node (Bitcoin Verde)", regexp.MustCompile(`(?i)^/Bitcoin Verde:`), true},
	// Software of another chain that still dials Bitcoin Cash's port: Bitcoin
	// itself (Core and Knots say /Satoshi:), Bitcoin ABC (eCash since 2020),
	// Bitcoin SV. None of them can hand this node a block of its chain.
	{"Other chain", regexp.MustCompile(`(?i)^/Satoshi:|Knots:|^/Bitcoin ABC:|Bitcoin SV|^/Bitcoin XT:|eCash`), false},
	// "(EB32.0)" is how Bitcoin Cash software states the block size it accepts:
	// a client no rule above knows that says it is still a Bitcoin Cash node.
	{"Node (other)", regexp.MustCompile(`\(EB[0-9]`), true},
}

func classifyBCH(subver string) (string, bool, bool) {
	for _, r := range bchRules {
		if r.re.MatchString(subver) {
			return r.name, r.relays, true
		}
	}
	return "", false, false
}

var bchAbouts = map[string][2]string{
	"Node (BCHN)":          {"Bitcoin Cash Node, the most widely run Bitcoin Cash software", "Bitcoin Cash Node"},
	"Node (bchd)":          {"bchd, a Bitcoin Cash node written in Go", "bchd"},
	"Node (BCH Unlimited)": {"BCH Unlimited, a Bitcoin Cash node", "BCH Unlimited"},
	"Node (Flowee)":        {"Flowee the Hub, a Bitcoin Cash node", "Flowee"},
	"Node (Knuth)":         {"Knuth, a Bitcoin Cash node", "Knuth"},
	"Node (Bitcoin Verde)": {"Bitcoin Verde, a Bitcoin Cash node written in Java", "Bitcoin Verde"},
	"Other chain":          {"Software of another chain (Bitcoin, eCash or Bitcoin SV): it cannot bring you a Bitcoin Cash block", "Another chain"},
}

// aboutBCH is the sentence and the short label for a peer the Bitcoin Cash
// rules named, or "", "" for one they did not.
func aboutBCH(subver, kind string) (string, string) {
	if _, _, ok := classifyBCH(subver); !ok {
		return "", ""
	}
	a := bchAbouts[kind]
	return a[0], a[1]
}
