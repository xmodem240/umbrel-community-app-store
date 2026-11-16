package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// fakeWalletKind is for a peer whose user agent names a wallet while it
// offers what no wallet can: blocks (NODE_NETWORK) or the encrypted v2
// transport, which no wallet had when these names were current. On the node
// this was written for, some 33,000 inbound sessions in a fortnight came as
// Bitcoin Wallet 4.x/5.x from 2016 (BlackBerry builds among them), MultiBit
// and breadwallet 0.6 - each version about as often as the next, gone after
// some 20 seconds, from over 800 addresses, and nearly all offering
// NETWORK and P2P_V2. The real Bitcoin Wallet 9 to 11 offered nothing. So
// this is a claim contradicted by an observation, and the kind says so
// rather than counting them as wallets. They still pass no blocks on to
// this node, so they keep the wallet's no-relay mark.
// "Disguised" rather than "Fake": it says what happens - the
// peer passes itself off as something else - without claiming why.
const fakeWalletKind = "Disguised"

// What Bitcoin Core started to offer, and in which version. A peer calling
// itself an older Core (or a Knots built on it) that offers one of these
// is not that version - except compact block filters on Knots, which served
// them before Core 0.21 did. On one node 23,567 sessions came as Core 0.7 to 0.18
// offering P2P_V2 - from Core 26, 2023 - each gone after about 25 seconds.
var serviceSince = []struct {
	name, what string
	major      int
	minor      int
}{
	{"P2P_V2", "encrypted v2 connections", 26, 0},
	{"COMPACT_FILTERS", "compact block filters", 0, 21},
	{"NETWORK_LIMITED", "the pruned-node service", 0, 16},
	{"WITNESS", "SegWit", 0, 13},
}

// Each of those in a word, for the line under the pill (tellOf).
var serviceShort = map[string]string{
	"encrypted v2 connections": "v2", "compact block filters": "block filters",
	"the pruned-node service": "pruning", "SegWit": "SegWit",
}

// The Bitcore library by name: /bitcore:1.1.0/, nothing after it.
var bitcoreLibrary = regexp.MustCompile(`^/bitcore:[0-9][0-9.]*/$`)

// Software with no version that ever spoke the v2 transport. libbitcoin only
// before 4: libbitcoin-network took BIP324 in August 2026 (#856), for the 4.0
// node, which offers P2P_V2.
var neverV2 = regexp.MustCompile(`(?i)^/(bitcore|bcoin|Classic|Bitcoin ABC|BUCash|bitcoinj|BitCoinJ|breadwallet|MultiBit|libbitcoin:[0-3]\.)`)

// anachronism returns what a peer offers that the software it names never
// had, as a sentence for "Probably:", or "".
func anachronism(subver string, services []string) string {
	has := map[string]bool{}
	for _, s := range services {
		has[s] = true
	}
	knots := strings.Contains(subver, "Knots:")
	if maj, mnr, ok := claimedNumbers(subver); ok {
		dmaj, dmnr := devRelease(subver, maj, mnr)
		for _, s := range serviceSince {
			if knots && s.name == "COMPACT_FILTERS" {
				continue
			}
			if has[s.name] && (dmaj < s.major || dmaj == s.major && dmnr < s.minor) {
				since := fmt.Sprintf("%d.%d", s.major, s.minor)
				if s.major > 0 {
					since = fmt.Sprint(s.major)
				}
				return fmt.Sprintf("not Bitcoin Core %d.%d - it offers %s, which Core %d.%d did not have (from Core %s)", maj, mnr, s.what, maj, mnr, since)
			}
		}
	}
	// Core 0.16 and later always offer NETWORK_LIMITED, pruned or not, so a
	// newer Core offering something but not that is not Core. Seen on one
	// node as a "/Satoshi:31.0.0/" fleet offering NETWORK and WITNESS only,
	// and as "/Satoshi:0.19.0.1/" offering WITNESS alone.
	if len(services) > 0 && !has["NETWORK_LIMITED"] && coreAtLeast(subver, 0, 16) {
		return "not Bitcoin Core - it lacks NETWORK_LIMITED, which every Core since 0.16 offers"
	}
	// The Bitcore library announces NETWORK and nothing else (bitcore-p2p's
	// version message defaults to services 1). Seen 21. to 26.10.2026: 921
	// sessions from 510 addresses named /bitcore:1.1.0/, 858 of them offering
	// WITNESS, NETWORK_LIMITED and P2P_V2 at today's chain height, most gone in
	// under five seconds; the rest announced NETWORK, like the library.
	if bitcoreLibrary.MatchString(subver) {
		var extra []string
		for _, sv := range services {
			if sv != "NETWORK" {
				extra = append(extra, sv)
			}
		}
		if len(extra) > 0 {
			return "not what it claims - it offers " + strings.Join(extra, ", ") + ", which the Bitcore library does not offer by default"
		}
	}
	if has["P2P_V2"] && neverV2.MatchString(subver) {
		return "not what it claims - it offers encrypted v2 connections, which that software never had"
	}
	return ""
}

// devRelease counts a development build as the release under way: 0.20.99
// as 0.21, 31.99 as 32, so a feature merged before the release is not held
// against it.
func devRelease(subver string, maj, mnr int) (int, int) {
	switch {
	case !devBuild.MatchString(subver):
	case maj == 0:
		mnr++
	case mnr == 99:
		maj, mnr = maj+1, 0
	}
	return maj, mnr
}

// protocolMismatch is a peer whose protocol version does not match the Core
// release it names: Core 0.21 to 31 send 70016, 32 and later 70017 or more,
// every release before sends 70015 or less. Development builds (x.99) are
// left alone: "0.20.99" was already 0.21 under way. On one node, in four
// days: 12,100 LinkingLion sessions naming Core 0.8 to 0.20 with 70016, a
// "/Satoshi:31.0.0/" fleet with 70015 - and no real Core node either way.
func protocolMismatch(subver string, version int) string {
	maj, mnr, ok := coreRelease(subver)
	if !ok || version == 0 || devBuild.MatchString(subver) {
		return ""
	}
	newer := maj >= 22 || mnr >= 21
	switch {
	case newer && version < 70016:
		return fmt.Sprintf("not Bitcoin Core - it speaks protocol %d, older than any Core since 0.21", version)
	case !newer && version >= 70016:
		return fmt.Sprintf("not Bitcoin Core - it speaks protocol %d, which Core %d.%d never did", version, maj, mnr)
	// Core 32 (tagged 21.10.2026) raised it to 70017 (node/protocol_version.h).
	case newer && maj <= 31 && version > 70016:
		return fmt.Sprintf("not Bitcoin Core - it speaks protocol %d, which Core %s never did", version, coreMajor(maj, mnr))
	// All 346 sessions naming Core 32 on one node, 21.10. to 02.11.2026, spoke 70017.
	case maj >= 32 && version < 70017:
		return fmt.Sprintf("not Bitcoin Core - it speaks protocol %d, older than Core %d", version, maj)
	}
	return ""
}

// skipsWtxidrelay is a peer naming Core 0.21 or later that finished its
// handshake without sending wtxidrelay. Every Core since 0.21 sends it right
// after the version message to a peer speaking 70016 or later
// (net_processing.cpp, WTXID_RELAY_VERSION; unchanged in 25.1 and 28.1), and
// getpeerinfo counts it under bytesrecv_per_msg. On the node this was written
// for, on 24.10.2026: all 18 outbound Core and Knots peers and 74 inbound ones
// sent it; of those that did not, 26 were Wasabi, two followed blocks for
// hours offering nothing, and two passed for nodes.
// Core sends it only when both sides speak 70016, so on a node older than
// 0.21 no peer sends it at all: anySent, whether any peer in the list sent
// one, keeps such a node from striking every modern peer.
func skipsWtxidrelay(subver string, version int, recv map[string]int64, anySent bool) bool {
	return anySent && recv != nil && recv["verack"] > 0 && recv["wtxidrelay"] == 0 &&
		version >= 70016 && coreAtLeast(subver, 0, 21)
}

// skippedHandshakeAbout names what the peer left out. sendaddrv2 is sent
// under the same condition as wtxidrelay, so it is named when it is missing too.
func skippedHandshakeAbout(recv map[string]int64) string {
	if recv["sendaddrv2"] == 0 {
		return "not Bitcoin Core - it left out wtxidrelay and sendaddrv2 when connecting, which it should have sent"
	}
	return "not Bitcoin Core - it left out wtxidrelay when connecting, which it should have sent"
}

// offersNothingAsCore is a peer naming Bitcoin Core 0.16 or later (or Knots)
// that offers no service at all. Those always offer NETWORK_LIMITED, pruned
// or not, so the name is borrowed. An older pruned Core could offer nothing. On the node this was written for:
// 6,954 sessions in four weeks, 27% of everything that said "/Satoshi:", from
// 620 addresses, 73% of them on Amazon's network, and not one ever delivered
// a block first. A nil list - Core did not say - is not this.
func offersNothingAsCore(subver string, services []string) bool {
	return services != nil && len(services) == 0 && coreAtLeast(subver, 0, 16)
}

// offersNothingAbout names the services Core always offers, rather than
// guessing what the peer is.
const offersNothingAbout = "not Bitcoin Core - it offers no services at all; every Core since 0.16 offers NETWORK_LIMITED"

// What a peer does, not what it says. Core pings every two minutes
// (PING_INTERVAL in net_processing.cpp) and asks a peer for addresses once per
// connection (m_getaddr_sent); a peer naming Core and doing either far more
// often runs other code. bitcoinj pings every two seconds, crawlers ask again
// and again. The limits leave a wide margin: a ping every 5 seconds, three
// requests. Not 10: a Core development build (31.99) was seen pinging every
// 10 seconds, still Core's code.
const (
	behaviourMinAge = 10 * 60 // seconds connected before a ping rate means anything
	pingFastSeconds = 5
	getaddrTooMany  = 3
	getaddrCrawler  = 10 // from here on the line calls it a crawler
)

// msgCount turns Core's bytes per message type back into a count, for
// messages that always have the same size. Core adds the 24-byte header on v1
// and, on v2, the message type plus BIP324's 20 bytes of length and tag
// (m_raw_message_size in net.cpp). The type is one byte for messages with a
// BIP324 short id, such as ping, and 13 bytes for those without, such as
// getaddr (V2_MESSAGE_IDS in net.cpp). Anything else, "detecting" included,
// is not counted.
func msgCount(bytes int64, payload int64, transport string, shortID bool) int64 {
	switch transport {
	// Core before 26 has no transport field and speaks only v1.
	case "v1", "":
		return bytes / (payload + 24)
	case "v2":
		if shortID {
			return bytes / (payload + 1 + 20)
		}
		return bytes / (payload + 13 + 20)
	}
	return 0
}

// handshakeOnly are the messages a peer sends to introduce itself and to answer
// our pings. A peer that has sent nothing else for ten minutes has asked for no
// header, address or transaction: not what any node, wallet or relay does.
var handshakeOnly = map[string]bool{
	"version": true, "verack": true, "feefilter": true, "ping": true, "pong": true,
	"sendheaders": true, "sendcmpct": true, "sendaddrv2": true, "wtxidrelay": true, "sendtxrcncl": true,
}

// quietAfterHandshake is true once a peer has been connected long enough to
// have asked for something and has sent only the handshake. recv is
// getpeerinfo's bytesrecv_per_msg; nil means the node did not report it. On one
// node, in 4,500 sessions of Core and Knots of ten minutes or more, none did.
func quietAfterHandshake(recv map[string]int64, connTime int64, now time.Time) bool {
	if recv == nil || recv["verack"] == 0 || !connectedFor(connTime, now, behaviourMinAge) {
		return false
	}
	for m, n := range recv {
		if n > 0 && !handshakeOnly[m] {
			return false
		}
	}
	return true
}

// quietCoreAbout is the last reason a peer naming Core is not Core: after the
// handshake it asked for nothing, not even for addresses. Only for full-relay
// peers - one that wants no transactions may be a block-relay-only connection,
// which has little to ask for - and it comes after every harder reason (a
// service Core always offers, a protocol it cannot speak). Observed, 21. to
// 26.10.2026 and in two older sets: of 4,500 sessions of Core and Knots of ten
// minutes or more, none stayed quiet.
func quietCoreAbout(subver string, relayTxes *bool, recv, sent map[string]int64, connTime int64, now time.Time) string {
	// Something must have been offered to it: on a node that relays nothing (blocksonly)
	// even a real Core has nothing to ask about.
	if !claimedCore.MatchString(subver) || !wantsTx(relayTxes) ||
		(sent["inv"] == 0 && sent["tx"] == 0) || !quietAfterHandshake(recv, connTime, now) {
		return ""
	}
	return "not Bitcoin Core - it asked for nothing after the handshake, not even for addresses"
}

// behaviourAbout is why a peer naming Core is not Core, judged by what it has
// sent, or "" when its behaviour fits. recv is getpeerinfo's bytesrecv_per_msg.
func behaviourAbout(subver, transport string, recv map[string]int64, connTime int64, now time.Time) string {
	if !claimedCore.MatchString(subver) || recv == nil {
		return ""
	}
	if n := msgCount(recv["getaddr"], 0, transport, false); n >= getaddrTooMany {
		// A few requests say only that it is not Core; "a crawler" takes many.
		// Seen 06.11.2026: the /Satoshi:31.0.0/ fleet, which pushes transactions,
		// asked three times in 37 hours and read as a crawler.
		if n < getaddrCrawler {
			return fmt.Sprintf("not Bitcoin Core - it asked for addresses %d times, Core asks once", n)
		}
		return fmt.Sprintf("a crawler - it asked for addresses %d times, Core asks once", n)
	}
	if !connectedFor(connTime, now, behaviourMinAge) {
		return ""
	}
	age := now.Unix() - connTime
	if n := msgCount(recv["ping"], 8, transport, true); n > 0 && age/n < pingFastSeconds {
		every := age / n
		unit := "seconds"
		if every <= 1 {
			every, unit = 1, "second"
		}
		return fmt.Sprintf("not Bitcoin Core - it pings every %d %s, Core every 2 minutes", every, unit)
	}
	return ""
}

// disguiseEvidence is every sign that a peer is not what its user agent says,
// from the same checks that make it Disguised. kind and walletAbout are what
// the name alone gave.
func disguiseEvidence(r rawPeer, kind, walletAbout string, anyWtxid bool, now time.Time) []string {
	var out []string
	if kind == "Wallet" && offersWhatNoWalletCan(r.ServicesNames) {
		_, guess, _ := strings.Cut(fakeAbout(r.Subver, walletAbout, r.ServicesNames), "\nProbably: ")
		out = append(out, guess)
	}
	if kind == "Wallet" && bitcoinjTooNew(r.Subver, r.Version) {
		out = append(out, bitcoinjTooNewAbout)
	}
	out = append(out,
		behaviourAbout(r.Subver, r.Transport, r.BytesRecvPerMsg, r.ConnTime, now),
		anachronism(r.Subver, r.ServicesNames),
		protocolMismatch(r.Subver, r.Version))
	if offersNothingAsCore(r.Subver, r.ServicesNames) {
		out = append(out, offersNothingAbout)
	}
	if skipsWtxidrelay(r.Subver, r.Version, r.BytesRecvPerMsg, anyWtxid) {
		out = append(out, skippedHandshakeAbout(r.BytesRecvPerMsg))
	}
	if givesOwnAddress(r) {
		out = append(out, ownAddressAbout)
	}
	return append(out, quietCoreAbout(r.Subver, r.RelayTxes, r.BytesRecvPerMsg, r.BytesSentPerMsg, r.ConnTime, now))
}

// A peer's version message says at which address it reached us. 0xB10C saw
// clients posing as Core 25 write their own address there instead (thread on
// clients ignoring fRelay, Delving Bitcoin, update of 19.10.2026). Of 224
// inbound peers on the node this was written for, none did (02.11.2026), so
// it only backs a disguise found otherwise. Localhost is not counted:
// CKPool's ckp2p sends 127.0.0.1 too.
const ownAddressAbout = "it says it reached us at its own address"

func givesOwnAddress(r rawPeer) bool {
	if !r.Inbound || r.AddrLocal == "" {
		return false
	}
	us, ok1 := ipOf(r.AddrLocal)
	them, ok2 := ipOf(r.Addr)
	return ok1 && ok2 && us == them && us.IsGlobalUnicast() && !us.IsPrivate()
}

// withAlso adds each reason the about does not give yet as an "Also:" line,
// the evidence without its verdict ("not Bitcoin Core - it lacks ..." says
// "it lacks ..."): the verdict is the Probably line's.
func withAlso(about string, reasons []string) string {
	for _, why := range reasons {
		if why == "" {
			continue
		}
		if _, evidence, ok := strings.Cut(why, " - "); ok {
			why = evidence
		}
		if strings.Contains(about, why) {
			continue
		}
		if !strings.HasPrefix(why, "it ") {
			why = "it " + why
		}
		about += "\nAlso: " + why
	}
	return about
}
