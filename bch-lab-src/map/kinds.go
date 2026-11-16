package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// What kind of software a peer runs, read off the user agent it sent in its
// version message.
//
// This is what the peer SAYS it is, and a user agent is a free-text field the
// other side chooses. On the node this was written for, four connections
// announced themselves as "/Sat0shi:31.0.0/" - Bitcoin Core with a zero in
// place of the o - from rented machines in five regions. So the kind is a
// label, not evidence.
//
// The counterweight is in the three observed flags on Peer, which nobody can
// write into a string: what the peer offers, whether it wants transactions,
// and whether Core has ever learned which chain it is on. A peer calling
// itself Core while offering nothing and having no known chain is the
// interesting case, and it is visible only because the two are shown side by
// side.
//
// The name answers the question somebody actually has, which is not "what
// software is this" but "is this a node that could hand me a block". So the
// class comes first and the software after it: "Node (Core)", not "Core".
//
// Order matters: the first match wins, so a more specific pattern goes above
// a more general one (Knots before Core - a Knots user agent contains
// "Satoshi" too).
//
// relays says whether this kind of software passes blocks on at all. It is the
// same distinction Bitcoin Lab paints red in its peer list, and it has to stay
// the same in both apps: a colour that means two different things in two
// windows of the same node is worse than no colour.
//
// A wallet has nothing to relay, a crawler is there to look, an indexer serves
// somebody else's wallet. Measured across 2,270 peers on one node: 688 of them
// ran software like this, 790 observations between them, and not one block
// delivered first. Unknown software counts as relaying - not knowing is not the
// same as knowing it cannot.
var kindRules = []struct {
	name   string
	re     *regexp.Regexp
	relays bool
}{
	// Bitcoin Core's name, misspelt: "/Sat0shi:" with a zero, "/Satoshi2:",
	// "/SatoshiX:", or "Satoshi:22.0.0" without the slashes Core always sends.
	// Real Core writes exactly "/Satoshi:x.y.z/". On the node this was written
	// for, "/Sat0shi:31.0.0/" alone came 475 times in a fortnight. First, so
	// nothing below reads it as Core. What it does is unknown, so it is not
	// marked as unable to relay.
	{"Disguised", regexp.MustCompile(`^/Sat0shi:|^/Satoshi[0-9X]+:|^Satoshi:`), true},
	// Mining pool software speaking the p2p protocol, or a node whose operator
	// says in the comment part of the agent - the bit in brackets - that it
	// belongs to a pool, e.g. "/Satoshi:29.1.0(PyBLOCK-POOL)/Knots:20250903/".
	// Only the brackets count: outside them "pool" is also in "mempool".
	// "mempool" inside the brackets does not count: "(mempool.guide)" is a
	// node's own label, not a pool (seen on the node this was written for).
	// /CKCoinD:/ is ckcoind, ckolivas' mining fork of Core (UA_NAME in its
	// clientversion.cpp). /ckp2p:/ is ckpool's own block relay: ckp2p.c on
	// the p2pcrawler and nodrop branches of github.com/ckolivas/ckpool (not
	// on master, which is why it was once taken for someone else's).
	// P2Pool's bitcoind connection says /P2Pool:x/ (p2pool/bitcoin/p2p.py), and
	// a name that is itself a pool - /pydroid-pool:3/ - is one too.
	{"Pool node", regexp.MustCompile(`(?i)ckpool|^/ckp2p:|^/CKCoinD:|\([^)]*pool[^)]*\)|^/P2Pool:|^/[^:/(]*pool[^:/(]*:`), true},
	// Clients of other chains that still dial Bitcoin's port.
	// Bitcoin Classic's "(EB8)" is Bitcoin Cash's block size; SuperBitcoin,
	// Irium and VDS are forks and altcoins of their own. Bitcoin Purity
	// (github.com/saltduck/bitcoinpurity), a fork of Knots, made BIP110
	// permanent in a hard fork at block 961637 with a difficulty of its own;
	// it says "/Satoshi:29.4/Purity:1.1.0/" (FormatSubVersion in
	// src/clientversion.cpp). On the node this was written for, 21. to
	// 29.10.2026: 269 sessions, all offering its bit 25 and BIP110's bit 27.
	{"Other chain", regexp.MustCompile(`(?i)Bitcoin ABC|BUCash|Bitcoin SV|BCHUnlimited|Bitcoin XT|Classic:|\(EB[0-9]|SuperBitcoin|iriumd|vds[_ ]|^/Aurum:|/Purity:|^/dexcash-nodes:`), false},
	// Network scanners, in two flavours: the ones run as a service and the
	// ones run by universities. Both announce themselves honestly.
	// Pool Detective, MIT's Digital Currency Initiative watching what mining
	// pools do: its block observer speaks btcd's old wire library and adds
	// "Satoshi:0.19" to the library's name, "/btcwire:0.4.1/Satoshi:0.19/"
	// (blockobserver/coinparam/bitcoin.go in mit-dci/pooldetective).
	// /trinetra:/ has no public source and stays "Other" (/p2p-probe:/, no
	// source either, is a crawler by what its name says):
	// in peer-observer's record of the node this was written for (21. to
	// 29.10.2026) each only did the handshake and left within a second, every
	// time (39 and 3 sessions), but one node is no rule. /rske-research:/ and /acsrg-research:/ say
	// what they are; the second took 81,563 transactions in one session.
	// /dataforge-observer:/ only listened to what was announced.
	// DaeConn, a measurement tool for Bitcoin-derived networks, says
	// "/connector/" (app/network/scanner.py in DogukanBaysal/DaeConn).
	{"Research scanner", regexp.MustCompile(`(?i)kit\.edu|dsn\.tm|dsn\.kastel|\.ac\.|uni-|^/btcwire:[^/]*/Satoshi:|^/rske-research:|^/acsrg-research:|^/dataforge-observer:|^/connector/$`), false},
	// Crawlers, seeders and node counters: they collect addresses and look,
	// and several of them say so only as "scan", "seeder", "census" or
	// "monitor" - on one node 953 sessions of /bitcoin-seeder/ and 372 of
	// /btc-range-scan/ used to land in "Other". /cryptexplorer:/,
	// /BlockSail:/ and /your-fork:/ have no public source and stay "Other",
	// although on that node they asked for addresses and nothing else
	// (peer-observer, 21. to 29.10.2026); /topology-probe:/ and the other
	// probes are crawlers by what their names say. BIP-110 Observer collects peer
	// user agents and services (app/crawler.py in decentralizedb/bip110-observer).
	// nodes.mom.market and bitcoinstats.com listened to what was announced and
	// asked for nothing in 0xB10C's list of 15.05.2023 (entities.toml on the
	// 2023-05-inv-eaters branch of bitcoin-noc/banlist). bitcoin-peers, a
	// toolkit with a crawler, says "/bitcoin-peers:<version>/"
	// (connection/src/connection/configuration.rs in nyonson/bitcoin-peers);
	// the Logos peer list checks onion peers for the Logos wallet
	// (crawler/src/p2p.rs in logos-co/logos-bitcoin-peer-list).
	{"Crawler", regexp.MustCompile(`(?i)bitnodes|bluematt-tokio-client|metrika|nodemap|crawl|scan|seeder|census|monitor|observatory|sonar|scout|nebula|watch|listener|argus|readonly|bitdash|logosnaut|dnsseed|survey|lionseed|gnoban|^/p2p-observer:|^/p2p-extractor:|^/bip324-probe:|^/topology-probe:|^/p2p-probe:|^/probe:|^/BTC-Nodes:[^/]*/Probe/|^/btc-peers:|^/bitcoin-peers:|^/logos-peer-list:|^/bip110-observer:|^/nodes\.mom\.market:|^/btcwire:[^/]*/bitcoinstats\.com:`), false},
	// Address indexers for wallets: they follow the chain but relay nothing.
	// "/mempool" at the start of a part of the agent: "/mempool/" and
	// "/mempool.space:1.0/" are the indexer, "(mempool.guide)" is a node label.
	// NBXplorer, BTCPay Server's indexer, names itself "NBXplorer-" and a
	// random number (Backend/Indexer.cs in dgarage/NBXplorer).
	{"Indexer", regexp.MustCompile(`(?i)electrs|electrum|esplora|/mempool|^NBXplorer-|^/symphony:`), false},
	// SPV and mobile wallets. bitcoinj is the Android wallet's library. Most
	// of them ask a node for their transactions through a bloom filter
	// (BIP37), which Core leaves off by default: at such a node they leave
	// within a second (peer-observer, 21. to 24.10.2026: Bitcoin Wallet a
	// median 0.1 s, BRD 0.3 s), at one that offers it they stay. /WalletKit:/
	// is Unstoppable Wallet on iPhone (Peer.swift in
	// horizontalsystems/BitcoinCore.Swift), /BitcoinKit:/ the same wallet on
	// Android (VersionMessage.kt in horizontalsystems/bitcoin-kit-android).
	{"Wallet", regexp.MustCompile(`(?i)bitcoinj|breadwallet|/bread:|bither|multibit|wasabi|Bitcoin Wallet|^/blueberry:|^/winnow:|^/BitcoinKit:|^/WalletKit:|^/Ladybug:|^/hodlwallet:|/spvwallet:`), false},
	// Lightning wallets on lnd that follow the chain through neutrino, its
	// BIP157 client: Zeus and Blixt among them. Named apart from the other
	// light clients so the wallets among the peers show as wallets. In
	// peer-observer's record of the node this was written for, 21. to
	// 24.10.2026: 403 sessions from 78 addresses, more than any other wallet
	// besides Wasabi.
	// lit, the Lightning node MIT's Digital Currency Initiative built with an
	// SPV client of its own, says "/btcwire:0.4.1/lit:v0.1/" (uspv/init.go
	// in mit-dci/lit, last changed in 2022).
	{"Lightning wallet", regexp.MustCompile(`(?i)neutrino|/lit:v`), false},
	// Other BIP157 light clients: Kyoto, which announces itself as
	// "Rust BIP-157". btcwire alone is only
	// the Go p2p library, which says nothing about the program using it, so it
	// stays out here and is caught by "Other" below.
	// VerifiedX, a sidechain that holds bitcoin: each of its validators keeps
	// eight Bitcoin peers to follow the chain itself, asking for headers and
	// now and then a block, never a transaction (BtcSources.cs and
	// BtcLightClient.cs in VerifiedXBlockchain/VerifiedX-Core, from 9.0 of
	// 24.10.2026). On the node this was written for, 12 of 238 peers that
	// day, all inbound, from 12 addresses at six hosting companies.
	// nakamoto, cloudhead's light client on compact block filters, says
	// "/nakamoto:0.3.0/" (p2p/src/fsm.rs). /multiseed:/ has no public source and
	// stays "Other"; on that node it stayed 52 minutes asking for headers and loading a bloom
	// filter, and took compact filter headers (peer-observer, 27.10.2026).
	// EMPORION, renamed SYMMARTYS, fetches headers to verify OpenTimestamps
	// proofs (bitcoin_headers.py in alexar76/emporion, under the old and new
	// package names). Both versions send their own name with version 0.3.
	// Bitflash's wallet speaks Bitcoin P2P itself and loads only headers and
	// compact filter hashes from peers that offer NODE_COMPACT_FILTERS
	// (docs.bitflash.network/atomic-swaps); that "/Bitflash:btc-light/" is
	// its name was not found in a source (11.11.2026).
	{"Light client", regexp.MustCompile(`(?i)BIP-157|^/bip157-fetch:|kyoto|^Yuki /|^/VerifiedX:|^/nakamoto:|^/bitcoin-s:|^Stacks/|/CaribouSPV:|^/EMPORION:|^/SYMMARTYS:|^/Bitflash:btc-light/`), false},
	// A small Utreexo node. Named so it is not lost in "Other", and counted as
	// relaying like any node - Bitcoin Lab has no rule for it either.
	{"Node (Floresta)", regexp.MustCompile(`(?i)floresta`), true},
	// Other full-node software, named rather than left in "Other".
	{"Node (btcd)", regexp.MustCompile(`(?i)btcd:`), true},
	{"Node (libbitcoin)", regexp.MustCompile(`(?i)libbitcoin`), true},
	{"Node (bcoin)", regexp.MustCompile(`(?i)/bcoin:`), true},
	{"Node (Gocoin)", regexp.MustCompile(`(?i)gocoin`), true},
	// Bitcoin Ghost (github.com/bitcoin-ghost/ghost): ghostd, a fork of Core
	// v30, UA_NAME "Ghost" in clientversion.cpp.
	{"Node (Ghost)", regexp.MustCompile(`^/Ghost:`), true},
	// BitcoinRocks uses RocksDB and compressed block storage, retaining
	// Bitcoin consensus (UA_NAME and CMakeLists.txt in BitcoinRocks-Core/BitcoinRocks).
	{"Node (BitcoinRocks)", regexp.MustCompile(`^/BitcoinRocks:`), true},
	// Full nodes written from scratch, each sending its own name (checked in
	// its source): the hashhog fleet of ten languages (github.com/hashhog),
	// reardencode/rbitcoin, AmazingAng/rbtc, bitcoinecho/bitcoin-echo,
	// BobClawblaw/bitcoinmachinecode, kernel-node/kernel-node, tbcd, the
	// Hemi network's Bitcoin daemon (hemilabs/heminetwork),
	// illuminodes/mantle, gosuda/bitcoin-rs, epochbtc/satd and
	// yuvicc/bitcoinkernel-node. hashhog's ouroboros now says
	// "/ouroboros:0.1.0/" (src/ouroboros/peer.py), and chjj/mako, a full node in
	// C, "/mako:0.0.0/" (include/mako/net.h). Three more have no public source and stay
	// "Other", although they acted as full nodes on the node this was written for (peer-observer, 21. to
	// 29.10.2026): /Prometheus:/ and /BTC-Nodes:/ were among its own outbound
	// peers, offering blocks (NETWORK) and relaying transactions for hours;
	// /Natasha:/ offers the same services and sent headers. BTC-Nodes' other
	// names, "/BTC-Nodes:<date>/Sonar/" and ".../Probe/", are caught as a
	// crawler first.
	{"Node (other)", regexp.MustCompile(`(?i)^/(CamlCoin|rustoshi|LunarBlock|Haskoin|beamchain|blockbrew|clearbit|nimrod|hotbuns|bitcoin-hybrid|ouroboros|mako|rbitcoin|rbtc|BitcoinEcho|BitcoinMachineCode|kernel-node|bitcoinkernel-node|tbcd|mantle|bitcoin-rs|satd):|/utreexod:`), true},
	// GrayersDad: see its line in about.go.
	{"Node (Knots)", regexp.MustCompile(`(?i)Knots|^/GrayersDad:`), true},
	{"Node (Core)", regexp.MustCompile(`^/Satoshi:`), true},
}

var coreNameWithSuffix = regexp.MustCompile(`^/(Satoshi[0-9X]+):`)

// misspeltCore says how a name imitating Core's differs from it, for the
// three spellings the Disguised rule catches, or "".
func misspeltCore(subver string) string {
	switch {
	case strings.HasPrefix(subver, "/Sat0shi:"):
		return "it writes Sat0shi with a zero"
	case coreNameWithSuffix.MatchString(subver):
		return "Core calls itself /Satoshi:, this one /" + coreNameWithSuffix.FindStringSubmatch(subver)[1] + ":"
	case strings.HasPrefix(subver, "Satoshi:"):
		return "Core writes its name between slashes: /" + strings.Trim(subver, "/") + "/"
	}
	return ""
}

var coreVersion = regexp.MustCompile(`[Ss]at[o0]shi[0-9X]*:([0-9.]+)`)

var claimedCore = regexp.MustCompile(`^/Satoshi:([0-9]+)\.([0-9]+)`)

// coreRelease reads a user agent that names a real Core release number:
// 0.x up to 0.21, then 22 and later. "/Satoshi:1.14.5(2x)/" (btc1) and other
// numbers Core never used return ok == false and are left alone.
func coreRelease(subver string) (maj, mnr int, ok bool) {
	maj, mnr, ok = claimedNumbers(subver)
	return maj, mnr, ok && (maj == 0 || maj >= 22)
}

// claimedNumbers is the major and minor number after "/Satoshi:", whether or
// not Core ever had that version.
func claimedNumbers(subver string) (maj, mnr int, ok bool) {
	m := claimedCore.FindStringSubmatch(subver)
	if m == nil {
		return 0, 0, false
	}
	maj, _ = strconv.Atoi(m[1])
	mnr, _ = strconv.Atoi(m[2])
	return maj, mnr, true
}

func coreAtLeast(subver string, major, minor int) bool {
	maj, mnr, ok := coreRelease(subver)
	return ok && (maj > major || maj == major && mnr >= minor)
}

// devBuild is a Core development build: 0.20.99 (0.21 under way) or 31.99.0.
var devBuild = regexp.MustCompile(`^/Satoshi:[0-9]+\.([0-9]+\.)?99[./]`)

// noAgentKind is a peer that has not told its name. On one node that was
// 21,830 sessions in a fortnight, from 2,516 addresses, typically gone after
// 16 seconds: connections that only checked the port, or that are still
// being set up when the page asks.
const noAgentKind = "No user agent"

// kindOf returns the family name, noAgentKind for a peer that sent no user
// agent at all, and "Other" for one whose agent matches nothing here - which
// is a normal answer, not a failure: the network runs plenty of software this
// list has never heard of.
func kindOf(subver string) string {
	name, _ := classify(subver)
	return name
}

// relaysBlocks is false only for software known not to pass blocks on. Anything
// unrecognised, and anything that sent no user agent at all, counts as relaying.
func relaysBlocks(subver string) bool {
	_, relays := classify(subver)
	return relays
}

// mempoolWord is removed before the kind rules look for "pool", so that a
// "(mempool.guide)" label does not make a node a pool node.
var mempoolWord = regexp.MustCompile(`(?i)mempool`)

var bracketed = regexp.MustCompile(`\([^)]*\)`)

// The kinds that read only the agent outside the brackets. Bitcoin Lab's
// test reads this list, so both apps agree on it.
var readsBareName = map[string]bool{"Research scanner": true, "Crawler": true, "Indexer": true, "Wallet": true, "Lightning wallet": true, "Light client": true}

// emptyName is a user agent with nothing in it but slashes, or a version
// without a name before it: "//" came 523 times in three days from two
// addresses, "/:1.1.0-dev+c1771608d/" ten times - nothing said, as with none.
var emptyName = regexp.MustCompile(`^/*$|^/:[^/]*/$`)

func classify(subver string) (string, bool) {
	if subver == "" || emptyName.MatchString(subver) {
		return noAgentKind, true
	}
	// The bracketed comment is the operator's own label. It may say a node
	// belongs to a pool, but a word like "monitor" or "electrs" in it says
	// what the operator runs beside it, not what the peer is: those kinds
	// read only the name outside the brackets.
	if bchMode {
		if name, relays, ok := classifyBCH(subver); ok {
			return name, relays
		}
	}
	bare := bracketed.ReplaceAllString(subver, "")
	for _, r := range kindRules {
		s := subver
		if r.name == "Pool node" {
			s = mempoolWord.ReplaceAllString(subver, "")
		} else if readsBareName[r.name] {
			s = bare
		}
		if r.re.MatchString(s) {
			return r.name, r.relays
		}
	}
	return "Other", true
}

// coreMajor is the release number people say: 0.21 for 0.x, 31 after.
func coreMajor(maj, mnr int) string {
	if maj == 0 {
		return fmt.Sprintf("0.%d", mnr)
	}
	return fmt.Sprint(maj)
}
