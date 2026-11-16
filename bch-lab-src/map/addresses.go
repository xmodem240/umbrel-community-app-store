package main

import (
	"net/netip"
)

// LinkingLion opens connections to many nodes from a few address ranges and
// listens to transaction announcements, which may let it link transactions to
// the IP addresses of nodes. Named and tracked by 0xB10C, who runs
// peer-observer (github.com/peer-observer/peer-observer); the ranges are the
// ones its shared/src/util.rs checked at first, announced by AS54098. On the node this
// was written for, over 24 days, they held 2.1% of all connection time - about
// 4 of some 180 peers at any moment - but came back so often, each time from a
// new address and gone after 1.6 minutes on average, that 62% of all addresses
// Bitcoin Lab ever saw were theirs. Nearly all used the old wallet names
// fakeWalletKind already catches.
// It stays Disguised: the kind says what it does, the about says who.
// Current ranges from the bnoc banlist (github.com/bitcoin-noc/banlist,
// entities/linkinglion.toml since the split of 02.11.2026, compared on
// 06.11.2026; the ranges are the same as after PR #4 of 16.09.2026): the IPv6
// range is one /64, not the
// provider's whole /32, and two single hosts come on top. One of them,
// 104.234.118.2, made 5,822 sessions on the node this was written for, with 82
// different fake user agents from that one address.
var linkingLion = []netip.Prefix{
	// used from 2025-12-05
	netip.MustParsePrefix("143.20.137.0/24"), netip.MustParsePrefix("31.58.215.0/24"),
	netip.MustParsePrefix("87.229.79.0/24"), netip.MustParsePrefix("2602:f5c0:0:ace::/64"),
	netip.MustParsePrefix("104.234.118.2/32"), netip.MustParsePrefix("69.17.52.1/32"),
	// used until the end of 2025
	netip.MustParsePrefix("162.218.65.0/24"), netip.MustParsePrefix("209.222.252.0/24"),
	netip.MustParsePrefix("91.198.115.0/24"), netip.MustParsePrefix("2604:d500:4:1::/64"),
	// listed by bnoc, no longer announced since 2026-03-30
	netip.MustParsePrefix("199.116.84.0/24"),
}

func isLinkingLion(addr string) bool {
	a, ok := ipOf(addr)
	if !ok {
		return false
	}
	for _, p := range linkingLion {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// University scanners that borrow a wallet's name instead of saying what they
// are. Both are among the research hosts in 0xB10C's peer-observer web UI
// (isLikelySpy, 2026-04-23), and the reverse DNS of each points back to its
// university. 147.229.8.240 came 13 times to the node this was written for
// between 16.10. and 02.11.2026, as /bitcoinj:0.16.2/Bitcoin Wallet:9.26/
// and /bitcoinj:0.16.5/Bitcoin Wallet:10.26/, offering nothing.
var researchHosts = map[netip.Addr]struct{ who, host string }{
	netip.MustParseAddr("147.229.8.240"): {"Brno University of Technology", "anton1.fit.vutbr.cz"},
	netip.MustParseAddr("161.53.40.241"): {"University of Rijeka", "pollux.riteh.hr"},
}

func researchHostOf(addr string) (who, host string, ok bool) {
	a, ok := ipOf(addr)
	if !ok {
		return "", "", false
	}
	h, ok := researchHosts[a]
	return h.who, h.host, ok
}

// The credit is not repeated on every card: the hover tooltip and the Disguised
// explanation above the list carry it (LION_CREDIT in web/app.js).
const linkingLionAbout = "LinkingLion – connects to many nodes and may link transactions to their IP addresses"

// linkingLionQuiet is added once the peer has been connected for a while and
// has still only done the handshake: it listens and asks for nothing.
const linkingLionQuiet = "; it only listens"
