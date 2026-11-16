package main

import (
	"fmt"
	"regexp"
	"time"
)

func offersWhatNoWalletCan(services []string) bool {
	for _, s := range services {
		if s == "NETWORK" || s == "P2P_V2" {
			return true
		}
	}
	return false
}

// bitcoinjTooNew is a peer naming Bitcoin Wallet (Schildbach's Android app)
// that speaks a newer protocol than its bitcoinj ever has: bitcoinj's version
// message sends a constant, 70001 up to 0.14, 70012 in 0.15 and 70013 from
// 0.16 to 0.17.1 and master (ProtocolVersion CURRENT). The app has never set
// its own in fifteen years of history. Only bitcoinj up to 0.17 counts, so a
// later bitcoinj that raises the number is not taken for a fake, and only the
// app: another program on bitcoinj could change the number itself. On the node
// this was written for, in four days: every bitcoinj offering nothing sent
// 70013 or 70001, except two sessions of "Bitcoin Wallet 9.26" with 70016
// from one university address, connected for eleven days without asking for
// a single header.
const bitcoinjMaxProtocol = 70013

var walletOnOldBitcoinj = regexp.MustCompile(`(?i)^/bitcoinj:0\.(1[0-7]|[0-9])[./][^/]*/Bitcoin Wallet:`)

func bitcoinjTooNew(subver string, version int) bool {
	return walletOnOldBitcoinj.MatchString(subver) && version > bitcoinjMaxProtocol
}

const bitcoinjTooNewAbout = "not a wallet - speaks a newer protocol than this wallet ever did"

// stayedLong adds how long the peer has been connected once that is a day or
// more: the real wallet, a phone app, stayed 1.4 hours at most on the node
// this was written for, the fake one eleven days. Only said alongside the
// protocol, which is the proof - a phone left open for days is possible.
func stayedLong(connTime int64, now time.Time) string {
	if connTime <= 0 {
		return ""
	}
	days := int(now.Sub(time.Unix(connTime, 0)).Hours() / 24)
	switch {
	case days < 1:
		return ""
	case days == 1:
		return ", and has stayed connected for a day"
	}
	return fmt.Sprintf(", and has stayed connected for %d days", days)
}

// askedNothing says what a "wallet" never did: a real one asks for headers,
// blocks or transactions within seconds of connecting, and sets a bloom filter
// to hear about its own. Only said once the peer has been connected for a
// while, since one that has just arrived has not had the time. recv is
// getpeerinfo's bytesrecv_per_msg; nil means the node did not report it.
func askedNothing(recv map[string]int64, connTime int64, now time.Time) string {
	if recv == nil || !connectedFor(connTime, now, behaviourMinAge) {
		return ""
	}
	for _, m := range []string{"getheaders", "getblocks", "getdata", "filterload", "tx", "mempool"} {
		if recv[m] > 0 {
			return ""
		}
	}
	return ", and never asked for a header or a transaction"
}

// bitcoinjWhy is the second reason given next to the protocol: that the peer
// asked for nothing, which says more than how long it stayed, or - when it did
// ask for something - how long it stayed.
func bitcoinjWhy(r rawPeer, now time.Time) string {
	if why := askedNothing(r.BytesRecvPerMsg, r.ConnTime, now); why != "" {
		return why
	}
	return stayedLong(r.ConnTime, now)
}

// Wasabi Wallet 2.8 connects to nodes itself, through Tor unless the user
// turns Tor off ("Network anonymization"); since 2.8.2 (PR #15003, 29.08.2026)
// that holds for clearnet nodes too, so it reaches one from a Tor exit. Older
// versions and Tor switched off connect directly. Each connection
// names one of these Core versions at random (Constants.UserAgents in
// WalletWasabi/Helpers/Constants.cs, used by P2pConnectionManager.cs), offers
// nothing and asks for transactions. On the node this was written for, on
// 24.10.2026: 27 such peers at once, 23 of them from Tor exits, every version
// from this list and none outside it, each fetching transactions and headers
// for hours, some also compact block filters. In five weeks before: 1,420
// such sessions, 73% from Tor exits. A crawler borrowing one of these names
// usually turns transaction relay off; one that keeps it on is counted here
// as a wallet. Forks pick at random the same way, some with lists of their
// own in between (Zecure: 18 names from 0.20.0 to 28.1.0, each on this list
// or Ginger's; also Wasabito and AnonTx), so the about says "or a fork".
var wasabiAgents = map[string]bool{
	"/Satoshi:30.2.0/": true, "/Satoshi:30.0.0/": true,
	"/Satoshi:29.3.0/": true, "/Satoshi:29.2.0/": true, "/Satoshi:29.0.0/": true,
	"/Satoshi:28.1.0/": true, "/Satoshi:28.0.0/": true,
	"/Satoshi:27.2.0/": true, "/Satoshi:27.1.0/": true, "/Satoshi:27.0.0/": true,
	"/Satoshi:26.2.0/": true, "/Satoshi:26.1.0/": true, "/Satoshi:26.0.0/": true,
}

// Ginger Wallet, a fork of Wasabi, does the same with the older list Wasabi
// had before 2.5 (Constants.UserAgents in GingerPrivacy/GingerWallet), also
// through Tor unless it is turned off (on by default). On the node this was written for, in five weeks: 161 sessions
// with a name only on this list, offering nothing and relaying transactions,
// 89% from Tor exits, the names spread evenly over the list.
var gingerAgents = map[string]bool{
	"/Satoshi:26.0.0/": true, "/Satoshi:25.1.0/": true, "/Satoshi:25.0.0/": true, "/Satoshi:24.2.0/": true,
	"/Satoshi:24.1.0/": true, "/Satoshi:24.0.1/": true, "/Satoshi:24.0.0/": true, "/Satoshi:23.2.0/": true,
	"/Satoshi:23.1.0/": true, "/Satoshi:23.0.0/": true, "/Satoshi:22.1.0/": true, "/Satoshi:22.0.0/": true,
	"/Satoshi:0.21.2/": true, "/Satoshi:0.21.1/": true, "/Satoshi:0.21.0/": true, "/Satoshi:0.20.2/": true,
	"/Satoshi:0.20.1/": true, "/Satoshi:0.20.0/": true, "/Satoshi:0.19.1/": true, "/Satoshi:0.19.0.1/": true,
	"/Satoshi:0.19.0/": true, "/Satoshi:0.18.1/": true, "/Satoshi:0.18.0/": true, "/Satoshi:0.17.2/": true,
	"/Satoshi:0.17.1/": true, "/Satoshi:0.17.0.1/": true, "/Satoshi:0.17.0/": true, "/Satoshi:0.16.3/": true,
	"/Satoshi:0.16.2/": true, "/Satoshi:0.16.1/": true, "/Satoshi:0.16.0/": true,
}

// Wasabi also searches for nodes: each wallet runs ten crawlers that connect
// through Tor with the same random name and transaction relay off, take the
// address list and leave within 15 seconds (CreateCrawler in
// P2pConnectionManager.cs). Ginger has no such crawler. Both speak through
// NBitcoin, whose handshake sends sendaddrv2 and verack and nothing else
// (Node.cs): no wtxidrelay, no sendcmpct. In peer-observer's record of the
// node this was written for, 21. to 24.10.2026: 3,247 such searches, the 13
// names 228 to 289 times each, every one with start height 0 and asking for
// addresses, and 402 wallet connections, 20 to 41 per name. Another 101 with
// these names sent sendcmpct instead of sendaddrv2, unevenly spread: other
// software, left Disguised.

// virtu's p2p-crawler (github.com/virtu/p2p-crawler), which counts reachable
// nodes for research, sends one fixed version message: "/Satoshi:23.0.0/" at
// protocol 70015, no services, relay off, start height 0 (protocol.py). Core
// 23 speaks 70016. In peer-observer's record of the node this was written
// for, 21. to 24.10.2026: 6 sessions with exactly this, all inbound; the 27
// others under that name spoke 70016.
func p2pCrawlerSays(subver string, version int, services []string, relayTxes *bool) bool {
	return subver == "/Satoshi:23.0.0/" && version == 70015 && services != nil && len(services) == 0 &&
		refusesTx(relayTxes)
}

// behindCoreName is the kind and about for Wasabi, Ginger and Wasabi's peer
// search, or "", "". recv is bytesrecv_per_msg. NBitcoin speaks 70016.
func behindCoreName(subver string, version int, services []string, relayTxes *bool, recv map[string]int64) (kind, about, name, label string) {
	if p2pCrawlerSays(subver, version, services, relayTxes) {
		return "Crawler", "Claims: " + claimOf(subver) + "\nProbably: p2p-crawler, virtu's research crawler - its fixed version message: protocol 70015, no services, no transactions", "p2p-crawler", "Probably p2p-crawler"
	}
	if services == nil || len(services) > 0 || relayTxes == nil || version < 70016 {
		return "", "", "", ""
	}
	// Anything beyond NBitcoin's handshake is other software.
	if recv["wtxidrelay"] > 0 || recv["sendcmpct"] > 0 {
		return "", "", "", ""
	}
	claim := "Claims: " + claimOf(subver) + "\nProbably: "
	if !*relayTxes {
		if wasabiAgents[subver] && recv["sendaddrv2"] > 0 {
			return "Crawler", claim + fmt.Sprintf("Wasabi Wallet's peer search - one of its %d borrowed Core names, offers nothing, asks only for addresses", len(wasabiAgents)), "", "Probably Wasabi's search"
		}
		return "", "", "", ""
	}
	switch {
	case wasabiAgents[subver] && gingerAgents[subver]:
		return "Wallet", claim + "Wasabi or Ginger Wallet - a borrowed Core name both use, offers nothing, wants transactions", "Wasabi", "Probably Wasabi or Ginger"
	case wasabiAgents[subver]:
		return "Wallet", claim + fmt.Sprintf("Wasabi Wallet or a fork - one of its %d borrowed Core names, offers nothing, wants transactions", len(wasabiAgents)), "Wasabi", "Probably Wasabi Wallet"
	case gingerAgents[subver]:
		return "Wallet", claim + fmt.Sprintf("Ginger Wallet (or an old Wasabi) - one of its %d borrowed Core names, offers nothing, wants transactions", len(gingerAgents)), "Ginger", "Probably Ginger Wallet"
	}
	return "", "", "", ""
}
