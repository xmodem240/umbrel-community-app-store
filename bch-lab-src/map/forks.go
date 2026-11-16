package main

import (
	"slices"
	"strings"
	"time"
)

// Libre Relay (github.com/petertodd/bitcoin, branches libre-relay-v28.0 to
// v31.1) is Core under Core's own name with service bit 29 added
// (NODE_LIBRE_RELAY in protocol.h). Core lists that bit as "UNKNOWN[2^29]".
// Libre Relay opens extra connections to nodes advertising it and drops them
// at once when the bit is missing ("does not offer libre relay as expected").
// Garbageman (github.com/chrisguida/bitcoin, branches garbageman-v28,
// garbageman-v29, garbageman-29.3) is a Knots fork with the Knots part of
// its name commented out, so it says "/Satoshi:29.1.0/" or 29.3.0, and it
// advertises the same bit to draw Libre Relay nodes in and drop what they
// send. Libre Relay has no 29.3 and Garbageman nothing past 29, so the
// version tells them apart except up to 29.1. On the node this was written
// for, 21. to 24.10.2026: 21 connections with the bit from 12 addresses,
// 18 of them as 30.2.0, all gone straight after the version message.
const libreRelayService = "UNKNOWN[2^29]"

// libreRelay is the kind and about for a Core name offering bit 29, or "", "".
func libreRelay(subver string, services []string) (kind, about, label string) {
	if !slices.Contains(services, libreRelayService) {
		return "", "", ""
	}
	maj, mnr, ok := claimedNumbers(subver)
	if !ok {
		return "", "", ""
	}
	garbageman := "Claims: Libre Relay\nProbably: Garbageman - a Knots fork that poses as Libre Relay and drops what it gets"
	switch {
	case strings.Contains(subver, "Knots:"):
		// Libre Relay is Core; only a Garbageman built with its Knots name left in says Knots.
		return fakeWalletKind, garbageman, ""
	case maj >= 30:
		return "Node (Libre Relay)", "Libre Relay, a fork of Core that also relays transactions Core and Knots filter out", "Libre Relay (Core fork)"
	case maj == 29 && mnr >= 2:
		return fakeWalletKind, garbageman, ""
	}
	return "Node (Libre Relay)", "Libre Relay - or Garbageman, a Knots fork that poses as it", "Libre Relay or Garbageman"
}

// Service bits Knots gives to rules other than Bitcoin's (src/protocol.h on
// its 29.x-knots branch): bit 27 NODE_REDUCED_DATA, a node enforcing BIP110's
// ReducedData rules, bit 28 NODE_BLAKE2B, a node enforcing a hard fork to
// BLAKE2b proof of work (and ReducedData). Core lists them as UNKNOWN[2^n].
// In peer-observer's record of the node this was written for, 21. to
// 25.10.2026, with that node at height 973,026: 898 sessions offering bit 28
// from 406 addresses reported start heights around 978,000, a chain of their
// own; 2,719 offering bit 27 from 921 addresses reported around 961,000, the
// BIP110 chain, which went its own way. Neither can deliver a block of this
// node's chain, so both count as never first. The
// node had also dialled 33 and 18 of them itself, each holding an outbound
// slot for 40 to 65 seconds at the median, up to 22 minutes. Crawlers repeat
// bits they saw elsewhere (/bitcoin-seeder:0.01/ sent both), so only nodes and
// unknown software count.
// forkTipSlack is how many blocks behind the tip a peer's headers may be and
// still count as this chain: one block found while the answer was on its way,
// with room to spare.
const forkTipSlack = 6

const (
	reducedDataService = "UNKNOWN[2^27]"
	blake2bService     = "UNKNOWN[2^28]"
)

// forkOf is the flag and its hover text for a peer offering one of those
// bits, or "", "". A peer whose headers reach this node's tip follows this
// chain whatever it offers, and is not flagged: in Bitcoin Lab's record of the
// node this was written for, 3,945 of 3,947 sessions with bit 27 shared no
// header past about 961,000 while the node was thousands of blocks further;
// the other two, a home-built node, were at the tip. synced is the peer's
// synced_headers (nil or -1 while unknown), tip the highest of any peer.
func forkOf(services []string, kind string, synced *int64, tip int64) (string, string) {
	if kind != "Other" && !strings.HasPrefix(kind, "Node") {
		return "", ""
	}
	if synced != nil && *synced >= 0 && tip > 0 && tip-*synced <= forkTipSlack {
		return "", ""
	}
	has := map[string]bool{}
	for _, s := range services {
		has[s] = true
	}
	switch {
	case has[blake2bService]:
		return "BLAKE2b fork", "Offers Knots' BLAKE2b bit (service bit 28): it follows a hard fork to another proof of work, so it is on a chain of its own, not yours"
	case has[reducedDataService]:
		return "BIP110 fork", "Offers Knots' ReducedData bit (service bit 27): it enforces BIP110's rules, which put it on a chain of its own, not yours"
	}
	return "", ""
}

// Syncing: a peer that takes whole blocks from this node faster than the chain
// grows, and has not caught up, is a node still syncing - one just set up, or
// one started again. A block every ten minutes is 0.16 MB a minute. On one
// node, 21. to 26.10.2026, 418 inbound peers took 20 MB or more; 365 of them at
// 1 MB a minute or faster over their whole stay (median 5). The average over
// the whole stay is all a single look at getpeerinfo gives, so a peer that has
// reached the tip is left out by what Core says it has: the last block it
// shares with this node is within syncBehind blocks (a day) of the tip. Seen
// 26.10.2026: one peer 6 blocks behind that had taken 11.9 GB in 2.5 days at
// 3 MB a minute, another 27 behind with 3.8 GB - nodes at the tip that fetch
// blocks all the time - and three genuine ones with no common block yet.
const (
	syncMinBytes  = 20_000_000
	syncMinPerMin = 1_000_000.0
	syncBehind    = 144
)

// syncingFrom is how many megabytes of blocks an inbound peer has taken, in this session, from
// this node while it syncs, or 0. sent is bytessent_per_msg, synced is
// synced_blocks (nil or below zero: Core does not know), tip the highest
// header any peer shares.
func syncingFrom(inbound bool, sent map[string]int64, synced *int64, tip, connTime int64, now time.Time) int64 {
	b := sent["block"]
	if !inbound || b < syncMinBytes || connTime <= 0 {
		return 0
	}
	if synced != nil && *synced >= 0 && tip > 0 && tip-*synced < syncBehind {
		return 0
	}
	secs := now.Unix() - connTime
	if secs < 1 || float64(b)/(float64(secs)/60) < syncMinPerMin {
		return 0
	}
	return b / 1_000_000
}
