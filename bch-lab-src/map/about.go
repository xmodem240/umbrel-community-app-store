package main

import (
	"regexp"
	"strings"
	"time"
)

// What a piece of software is, in a sentence, for the peers somebody is
// likely to ask about. Only software that says who it is: the kind already
// covers the rest.
// label is the text cut to one line for the list; name, where set, is the
// chip the peer is counted under, for one program under many names.
var aboutRules = []aboutRule{
	{regexp.MustCompile(`(?i)seeder|dnsseed`), "A DNS seeder: collects addresses of reachable nodes for new nodes to start from", "DNS seeder", ""},
	// BTC Nodes (btcnodes.io), Rodrigo Martínez's successor to Bitnodes
	// (bitcointalk.org topic 5582226). Its public crawler config still says
	// /bitnodes.io:0.3/; that "/BTC-Nodes:<date>/Sonar/", ".../Probe/" and
	// ".../Delegator/" are its own was not found in a source, so only the name.
	// Before the census rule, which would take Sonar.
	{regexp.MustCompile(`^/BTC-Nodes:`), "Named like BTC Nodes (btcnodes.io), the successor to Bitnodes that maps reachable nodes", "Named like BTC Nodes", ""},
	// 21 Data Lab's own crawler speaks Bitcoin P2P and asks nodes for their
	// peer lists (21datalab.com/de/methodik); its user agent is not given
	// there, so only the name (11.11.2026).
	{regexp.MustCompile(`^/21datalab-crawler:`), "Named like 21 Data Lab's crawler, which counts reachable Bitcoin nodes (21datalab.com)", "Named like 21 Data Lab", ""},
	// Nebula, Dennis Trautwein's network crawler (ProbeLab), crawls Bitcoin
	// since 2.4.0 (github.com/dennis-tra/nebula/releases). Who runs a given
	// crawl it does not say. "nebula/2.4.1-…" and "/nebula:v3.0.0-…/".
	{regexp.MustCompile(`(?i)^/?nebula[/:]`), "Nebula, a network crawler by Dennis Trautwein (ProbeLab) that also maps Bitcoin nodes", "Nebula crawler", ""},
	// A Knots node under its operator's own name: only Knots defines service
	// bits 27 and 28 (src/protocol.h, knots20260508), and these offer them.
	{regexp.MustCompile(`^/GrayersDad:`), "Probably Bitcoin Knots under a name its operator chose", "Probably Knots", ""},
	{regexp.MustCompile(`(?i)census|survey|observatory|monitor|sonar`), "A node census: counts and watches reachable nodes", "Node census", ""},
	// One address, 142 sessions in four days, each gone after its version
	// message. Elastum was an exchange; elastum.io no longer resolves.
	{regexp.MustCompile(`(?i)/Elastum:`), "Names Elastum, a former exchange; talks to nodes through bitcoinj", "Names Elastum (bitcoinj)", ""},
	// 26-27.10.2026: one address, five sessions in 40 minutes under protocol
	// 70001 with no services, saying our node is 127.0.0.1:8332. Two ended after
	// the version message; three took 7 to 16 MB of blocks each. Not found
	// anywhere, so the text says only what it names itself.
	{regexp.MustCompile(`^BTC RPC Proxy `), "Names itself an RPC proxy for Bitcoin; no public source", "Names an RPC proxy", ""},
	{regexp.MustCompile(`(?i)/Bisq:`), "Bisq, a decentralised exchange; its wallet runs on bitcoinj", "Bisq exchange wallet", ""},
	{regexp.MustCompile(`^/P2Pool:`), "P2Pool, a peer-to-peer mining pool: miners share the work without a pool operator", "P2Pool mining pool", ""},
	{regexp.MustCompile(`^/BitcoinRocks:`), "BitcoinRocks, an experimental fork of Core using RocksDB and compressed block storage", "BitcoinRocks, a Core fork", ""},
	{regexp.MustCompile(`(?i)^/EMPORION:`), "EMPORION, a Bitcoin header client for verifying OpenTimestamps proofs; later renamed SYMMARTYS", "EMPORION header client", ""},
	{regexp.MustCompile(`(?i)^/SYMMARTYS:`), "SYMMARTYS, formerly EMPORION: follows Bitcoin headers to verify OpenTimestamps proofs", "SYMMARTYS header client", ""},
	{regexp.MustCompile(`^/bitcoin-peers:`), "bitcoin-peers, a Rust toolkit to connect to and survey Bitcoin peers", "bitcoin-peers crawler", ""},
	{regexp.MustCompile(`^/logos-peer-list:`), "Logos peer list, a crawler that checks onion peers for the Logos wallet", "Logos peer list", ""},
	{regexp.MustCompile(`^/mako:`), "mako, a Bitcoin full node written in C", "mako node (C)", ""},
	{regexp.MustCompile(`(?i)^/bip110-observer:`), "BIP-110 Observer, a crawler that collects peer user agents and advertised services", "BIP-110 Observer crawler", ""},
	// Only the name points to favillakey.io; the site documents no P2P
	// connections, so the kind stays Other.
	{regexp.MustCompile(`^/FavillaKEY:`), "Named like Favilla KEY, a Nostr and Bitcoin signing device (favillakey.io); what it connects for is not documented", "Named like Favilla KEY", ""},
	{regexp.MustCompile(`^/Ghost:`), "Bitcoin Ghost, a fork of Core v30 that filters data-carrying transactions; its operators can join a decentralised mining pool", "Bitcoin Ghost (Core fork)", ""},
	{regexp.MustCompile(`(?i)^/CamlCoin:`), "CamlCoin, a Bitcoin full node written in OCaml (hashhog)", "CamlCoin node (OCaml)", ""},
	{regexp.MustCompile(`(?i)^/rustoshi:`), "rustoshi, a Bitcoin full node written in Rust (hashhog)", "rustoshi node (Rust)", ""},
	{regexp.MustCompile(`(?i)^/LunarBlock:`), "LunarBlock, a Bitcoin full node written in Lua (hashhog)", "LunarBlock node (Lua)", ""},
	{regexp.MustCompile(`^/Haskoin:`), "A Bitcoin full node written in Haskell (hashhog)", "Haskoin node (Haskell)", ""},
	{regexp.MustCompile(`(?i)^/beamchain:`), "beamchain, a Bitcoin full node written in Erlang (hashhog)", "beamchain node (Erlang)", ""},
	{regexp.MustCompile(`(?i)^/blockbrew:`), "blockbrew, a Bitcoin full node written in Go (hashhog)", "blockbrew node (Go)", ""},
	{regexp.MustCompile(`(?i)^/clearbit:`), "clearbit, a Bitcoin full node written in Zig (hashhog)", "clearbit node (Zig)", ""},
	{regexp.MustCompile(`(?i)^/nimrod:`), "nimrod, a Bitcoin full node written in Nim (hashhog)", "nimrod node (Nim)", ""},
	{regexp.MustCompile(`(?i)^/hotbuns:`), "hotbuns, a Bitcoin full node written in TypeScript on Bun (hashhog)", "hotbuns node (TypeScript)", ""},
	{regexp.MustCompile(`(?i)^/(bitcoin-hybrid|ouroboros):`), "ouroboros, a Bitcoin full node written in Rust and Python (hashhog)", "ouroboros node (Rust)", ""},
	{regexp.MustCompile(`(?i)^/rbitcoin:`), "rbitcoin, a Bitcoin full node written in Rust (reardencode)", "rbitcoin node (Rust)", ""},
	{regexp.MustCompile(`(?i)^/rbtc:`), "rbtc, Bitcoin Core rewritten in Rust", "rbtc, Core in Rust", ""},
	{regexp.MustCompile(`(?i)^/BitcoinEcho:`), "Bitcoin Echo, a full node in plain C, meant to be frozen as it is", "Bitcoin Echo node (C)", ""},
	{regexp.MustCompile(`(?i)^/BitcoinMachineCode:`), "Bitcoin Machine Code, a node written in x86 assembly, entirely AI-generated", "Bitcoin Machine Code node", ""},
	{regexp.MustCompile(`(?i)^/kernel-node:`), "kernel-node, a node built on libbitcoinkernel, Core's validation engine as a library", "kernel-node (Core kernel)", ""},
	{regexp.MustCompile(`(?i)^/bitcoinkernel-node:`), "bitcoinkernel-node, a small full node built on libbitcoinkernel, Core's validation engine as a library", "bitcoinkernel-node", ""},
	{regexp.MustCompile(`(?i)^/satd:`), "satd, a Bitcoin full node written in Rust that works like Core", "satd node (Rust)", ""},
	{regexp.MustCompile(`(?i)^/tbcd:`), "tbcd, the Bitcoin daemon of the Hemi network", "tbcd, Hemi's daemon", ""},
	{regexp.MustCompile(`(?i)hemi-soak`), "A soak test of the Hemi network's Bitcoin code", "Hemi soak test", ""},
	// paulscode's release of 28.10.2026, knots31.1.knots-sha256.20261018:
	// Knots carried on after it hard-forked to BLAKE2b.
	{regexp.MustCompile(`/KnotsSHA256:`), "Knots (SHA256), paulscode's build of Knots that stays on the SHA256 chain", "Knots (SHA256), paulscode", ""},
	// github.com/richardkiss/pycoinnet; where it sends this name was not found.
	{regexp.MustCompile(`^/pycoinnet/`), "Named like pycoinnet, Richard Kiss's Python library for the Bitcoin protocol", "Named like pycoinnet", ""},
	{regexp.MustCompile(`(?i)^/blueberry:`), "Blueberry, BlueWallet's new wallet that talks to nodes directly", "Blueberry (BlueWallet)", ""},
	// Labels a node's setup puts in brackets after Core's name. Apollo's own
	// bitcoin.conf sets it (jstefanop/apolloapi-v2, backend/default-configs).
	{regexp.MustCompile(`\(FutureBit-Apollo-Node\)`), "Labelled by FutureBit Apollo, a home Bitcoin miner with a built-in full node", "FutureBit Apollo node", ""},
	// docker/my-dojo/bitcoin/restart.sh: -uacomment="Samourai Dojo $DOJO_VERSION_TAG".
	{regexp.MustCompile(`\(Samourai Dojo `), "Labelled by Samourai Dojo, the self-hosted backend for Samourai Wallet and other light wallets", "Samourai Dojo node", ""},
	// Where RoninDojo sets it was not found, so the text says only what it is.
	{regexp.MustCompile(`\(RoninDojo `), "Labelled RoninDojo, an installer and UI for Samourai Wallet's Dojo", "RoninDojo node", ""},
	// The bitcoin.conf in MiniBolt's Core and Knots pages: uacomment=MiniBolt node.
	{regexp.MustCompile(`(?i)\((Official )?MiniBolt[ _]node\)|; MiniBolt node\)`), "Labelled by MiniBolt, a step-by-step guide to building a Bitcoin node", "MiniBolt guide node", ""},
	// A 2017 Docker image of Bitcore set uacomment=bitcore (m00re/bitcore-docker);
	// these run newer Core, so the text goes by the name only.
	{regexp.MustCompile(`\(bitcore\)`), "Labelled bitcore, named like BitPay's Bitcore, an index server that runs on a node", "Labelled bitcore", ""},
	{regexp.MustCompile(`(?i)^/winnow:`), "Winnow, an iPhone wallet that talks to nodes directly", "Winnow iPhone wallet", ""},
	{regexp.MustCompile(`(?i)^/BitcoinKit:`), "BitcoinKit, a wallet library (Unstoppable Wallet on Android and others)", "BitcoinKit wallet library", ""},
	{regexp.MustCompile(`^/WalletKit:`), "Unstoppable Wallet on iPhone", "Unstoppable Wallet iOS", ""},
	{regexp.MustCompile(`(?i)^/Ladybug:`), "Ladybug, a wallet built on bitcoinj", "Ladybug wallet", ""},
	{regexp.MustCompile(`(?i)^/hodlwallet:`), "HODL Wallet, a phone wallet", "HODL Wallet", ""},
	{regexp.MustCompile(`^Yuki /`), "Yuki, a light client by Impervious built on Kyoto", "Yuki light client", ""},
	{regexp.MustCompile(`^/VerifiedX:`), "A VerifiedX validator: a sidechain that holds bitcoin and follows Bitcoin's chain for it", "VerifiedX validator", ""},
	{regexp.MustCompile(`(?i)^/LionSeed:`), "LionSeed, a DNS seeder that lists only the nodes of one fork", "LionSeed DNS seeder", ""},
	{regexp.MustCompile(`(?i)^/gnoban:`), "gnoban, a tool that scores nodes and bans the ones its operator does not want", "gnoban node scorer", ""},
	{regexp.MustCompile(`^libbtc `), "libbtc, a small C library for Bitcoin wallets and tools", "libbtc C library", ""},
	{regexp.MustCompile(`(?i)^/haskoin-btc:`), "Haskoin, a Haskell library for Bitcoin wallets and tools", "Haskoin library (Haskell)", ""},
	{regexp.MustCompile(`(?i)^/sashimi:`), "sashimi, small Unix tools for Bitcoin by josibake", "sashimi Unix tools", ""},
	{regexp.MustCompile(`(?i)^/bridge:`), "Probably castor, Floresta's Utreexo bridge node", "Probably castor bridge", ""},
	{regexp.MustCompile(`(?i)^/rust-sha2-p2p-client/`), "gnostr-bitcoin, a terminal monitor of the Bitcoin network from the gnostr project", "gnostr-bitcoin monitor", ""},
	{regexp.MustCompile(`(?i)^/Gnostr:`), "Names gnostr, the git+nostr tools", "Names gnostr", ""},
	{regexp.MustCompile(`(?i)^/Aurum:`), "Aurumcoin, an altcoin built on Bitcoin Core 0.12", "Aurumcoin altcoin", ""},
	{regexp.MustCompile(`(?i)/Purity:`), "Bitcoin Purity, a fork of Knots that made BIP110 permanent in a hard fork at block 961637", "Bitcoin Purity hard fork", ""},
	{regexp.MustCompile(`Bitcoin Wallet:`), "Bitcoin Wallet by Schildbach, an Android app", "Bitcoin Wallet (Android)", ""},
	// breadwallet-core sends "/bread:" with its own version, 2.1 since 2019
	// (BRPeer.h), also in BRD's later WalletKit (blockset-corp/walletkit).
	{regexp.MustCompile(`^/bread:`), "BRD's wallet core: the BRD app, shut down in 2022, or a wallet built on its code", "BRD wallet core", ""},
	{regexp.MustCompile(`(?i)breadwallet`), "BRD (breadwallet), a phone wallet shut down in 2022", "BRD wallet, closed 2022", ""},
	{regexp.MustCompile(`(?i)multibit`), "MultiBit, a desktop wallet discontinued in 2017", "MultiBit, ended 2017", ""},
	{regexp.MustCompile(`(?i)bither`), "Bither, a wallet for phone and desktop", "Bither wallet", ""},
	{regexp.MustCompile(`(?i)wasabi`), "Wasabi Wallet, a desktop wallet", "Wasabi Wallet", ""},
	// The library alone, without a wallet app's name after it. After the
	// wallet rules above, which also start with bitcoinj.
	{regexp.MustCompile(`(?i)^/bitcoinj:`), "bitcoinj, a Java library that wallets and other tools use to talk to nodes", "bitcoinj Java library", ""},
	// neutrino sends a version fixed in its own code, not lnd's: 0.12.0-beta
	// from neutrino 0.12 to 0.16 (lnd 0.13 to 0.20), 0.17.1 since neutrino
	// 0.17.1 (lnd 0.21, June 2026; useragent.go in lightninglabs/neutrino).
	// Zeus 13.2 ships lnd 0.21.4, Blixt 0.9 lnd 0.20, and neither changes the
	// name; lnd on a server in neutrino mode says the same, so an app is only an example.
	{regexp.MustCompile(`(?i)/neutrino:0\.17\.`), "lnd 0.21 or newer, without a node of its own (neutrino), e.g. Zeus", "lnd 0.21+ (neutrino)", "lnd 0.21+"},
	{regexp.MustCompile(`(?i)/neutrino:0\.12\.0-beta/`), "Older lnd, without a node of its own (neutrino), e.g. Blixt or an older Zeus", "Older lnd (neutrino)", "older lnd"},
	{regexp.MustCompile(`(?i)neutrino`), "Neutrino: Lightning wallets on lnd, such as Blixt or Zeus", "lnd wallet (neutrino)", ""},
	{regexp.MustCompile(`(?i)BIP-157|kyoto`), "Kyoto, a light client for wallets built with BDK", "Kyoto light client (BDK)", ""},
	{regexp.MustCompile(`^/Bitflash:btc-light/`), "Named like Bitflash's wallet, which follows the chain by headers and compact filters (docs.bitflash.network)", "Named like Bitflash", ""},
	{regexp.MustCompile(`(?i)floresta`), "Floresta, a lightweight Utreexo node", "Floresta Utreexo node", ""},
	{regexp.MustCompile(`(?i)electrs`), "electrs, the address index behind Electrum-style wallets", "electrs address index", ""},
	{regexp.MustCompile(`(?i)^/CKCoinD:`), "ckcoind, a mining fork of Bitcoin Core used with ckpool", "ckcoind (ckpool fork)", ""},
	{regexp.MustCompile(`(?i)bluematt-tokio-client`), "dnsseed-rust, the crawler behind a DNS seeder", "dnsseed-rust crawler", ""},
	{regexp.MustCompile(`(?i)ckp2p`), "ckp2p, ckpool's block relay: passes on blocks only, never transactions", "ckp2p block relay", ""},
	// Relay networks with a name of their own: bloXroute's gateway builds
	// "bloXroute Gateway <version>" (bxgateway, 2022); Matt Corallo's
	// RelayNode adapters send /RelayNetworkClient:42/ and
	// /RelayNetworkServer:42/ (2013-2017); Falcon was seen as
	// /Cornell-Falcon-Network:0.1.0/ (bips discussion, March 2017).
	{regexp.MustCompile(`(?i)^bloXroute Gateway`), "bloXroute Gateway, a relay network's gateway: passes on blocks and transactions", "bloXroute gateway", ""},
	{regexp.MustCompile(`^/RelayNetwork(Client|Server):`), "Matt Corallo's old Relay Network, which ran until 2017", "Old Relay Network", ""},
	{regexp.MustCompile(`^/Cornell-Falcon-Network:`), "Falcon, Cornell's block relay network from 2016", "Cornell's Falcon relay", ""},
	// Before NBitcoin, whose name is inside "OpenBitcoin".
	{regexp.MustCompile(`^/OpenBitcoin-crawler:`), "OpenBitcoin's crawler, a survey of reachable nodes for openbitcoin.com", "OpenBitcoin crawler", ""},
	{regexp.MustCompile(`(?i)NBitcoin`), "NBitcoin, a .NET Bitcoin library (behind BTCPay Server, among others)", "NBitcoin .NET library", ""},
	{regexp.MustCompile(`(?i)/bitcore:`), "Bitcore, BitPay's node and index server", "Bitcore (BitPay)", ""},
	// Core's private broadcast (v31): it opens a connection over Tor or I2P,
	// hands over one of its own transactions and leaves, introducing itself
	// with this fixed name and no services so nobody can tell which node sent
	// it (net_processing.cpp, IsPrivateBroadcastConn). The one seen on the
	// node this was written for came from a Tor exit, v2, offering nothing.
	{regexp.MustCompile(`^/pynode:0\.0\.1/$`), "Probably Bitcoin Core handing over a transaction privately, then leaving", "Probably Core's broadcast", ""},
	// m4ycon on bnoc (#198): it sweeps the P2A anchors of a fleet that minted
	// rune 1:0 in chains of transactions; on the node this was written for it
	// pushed unannounced 66-byte transactions, the size of such a sweep.
	{regexp.MustCompile(`(?i)^/p2aburn:`), "p2aburn, a bot that sweeps P2A anchors (outputs anyone may spend) behind a Runes minting fleet", "p2aburn anchor sweeper", ""},
	{regexp.MustCompile(`(?i)python-bitcoinlib`), "python-bitcoinlib, a Python library scripts use to talk to nodes", "python-bitcoinlib", ""},
	{regexp.MustCompile(`(?i)pyblock`), "PyBLOCK, a node dashboard", "PyBLOCK dashboard", ""},
	// Projects whose name matches; none of them publishes the exact string.
	// Urbit reaches Bitcoin through its %btc-provider (github.com/urbit/urbit);
	// Symbiosis Finance runs a Bitcoin bridge (symbiosis.finance/bridge-btc);
	// DexCash Network is a blockchain of its own (dexcash.org). curl is the
	// download tool and does not speak Bitcoin's protocol, so a peer naming it
	// borrows the name.
	{regexp.MustCompile(`^urbit$`), "Probably Urbit's Bitcoin software (%btc-provider)", "Probably Urbit", ""},
	{regexp.MustCompile(`/Symbiosis Bitcoin Bridge:`), "Probably the Bitcoin bridge of Symbiosis Finance, built on btcd's Go library", "Probably Symbiosis bridge", ""},
	{regexp.MustCompile(`^/curl:`), "Names curl, the download tool; curl itself does not speak Bitcoin's protocol", "Borrowed name (curl)", ""},
	{regexp.MustCompile(`^/dexcash-nodes:`), "Named like DexCash Network, a blockchain of its own", "Named like DexCash", ""},
	{regexp.MustCompile(`(?i)^/alt-bitnodes:`), "Named after Bitnodes, the public node map", "Named after Bitnodes", ""},
	{regexp.MustCompile(`(?i)bitnodes`), "Bitnodes, a public map of reachable nodes", "Bitnodes crawler", ""},
	// Seen from one netcup address, offering nothing and gone after a second;
	// the name fits a node map, but nothing public says whose.
	{regexp.MustCompile(`(?i)GlobalNodeMap`), "Probably a node map or counter", "Probably a node map", ""},
	// illuminodes/mantle: the node says "/mantle:0.1.0/", its replay tool,
	// which fetches mainnet blocks from peers to validate them, says
	// "/mantle-replay-iouring:0.1.0/" (crates/fetcher/src/lib.rs); beside it
	// "/mantle-discover:0.1.0/" collects addresses with getaddr
	// (replay/src/discover_peers.rs) and "/mantle-rank-peers:0.1.0/" times
	// how fast a node answers its version (replay/src/rank_peers.rs).
	{regexp.MustCompile(`(?i)^/mantle:`), "Mantle, a Bitcoin full node written from scratch in Rust (illuminodes)", "Mantle node (Rust)", ""},
	{regexp.MustCompile(`(?i)^/mantle-replay`), "Mantle's replay tool: fetches blocks from nodes to check them with Mantle, a Bitcoin node written from scratch in Rust", "Mantle replay tool", ""},
	{regexp.MustCompile(`(?i)^/mantle-discover`), "Mantle's peer finder: asks nodes for addresses of other nodes", "Mantle peer finder", ""},
	{regexp.MustCompile(`(?i)^/mantle-rank-peers`), "Mantle's speed test: times how fast a node answers", "Mantle speed test", ""},
	// btcd's wire package sends "/btcwire:0.5.0/" when a program sets no name
	// of its own (wire/msgversion.go), and its peer package's example sets
	// "peer" 1.0.0 (peer/example_test.go). On the node this was written for,
	// 21. to 25.10.2026, the bare name asked for addresses and headers and
	// wanted no transactions.
	{regexp.MustCompile(`^/btcwire:[^/]*/$`), "A program on btcd's Go library that set no name of its own", "btcd library, no own name", ""},
	{regexp.MustCompile(`^/btcwire:[^/]*/peer:1\.0\.0/$`), "Probably a program built from the example code in btcd's Go library, keeping its name", "Probably btcd sample code", ""},
	// RESET (Trac-Systems/reset) is a coin clocked by Bitcoin's blocks; a
	// share of its reward goes to whoever brings in a new block first. Its
	// agents fetch from mempool.space unless told otherwise and its code has
	// no P2P client, so these names are a guess from the name, the behaviour
	// and the timing: its repository was created on 15.10.2026, and Bitcoin
	// Lab's record of the node this was written for, from 13.9., shows the
	// first reset-* name on 20.10.; by 25.10. there were seven, asking only
	// for headers, most wanting no transactions.
	{regexp.MustCompile(`(?i)^/reset-`), "Probably a header client for RESET, a coin clocked by Bitcoin's blocks that pays a small reward to whoever brings in a new block first", "Probably a RESET client", "RESET"},
	// JWRD Computing (jwrd.net) maintains its own bitcoind of the old 0.5
	// line, which writes version 99999 as 0.9.99.99; nothing public says it
	// sends this name.
	{regexp.MustCompile(`^/jwrd\.net:`), "Probably a node on JWRD Computing's own bitcoind, a fork of an early version", "Probably JWRD's bitcoind", ""},
	{regexp.MustCompile(`^/btcwire:[^/]*/Satoshi:`), "Pool Detective, by MIT's Digital Currency Initiative: watches what mining pools do", "MIT's Pool Detective", "Pool Detective"},
	{regexp.MustCompile(`(?i)/lit:v`), "lit, a Lightning node by MIT's Digital Currency Initiative, no longer developed", "lit Lightning node (MIT)", ""},
	// observer/src/protocol.rs in 0xB10C/p2p-observer, work in progress in
	// October 2026; extractors/p2p in peer-observer/peer-observer, which a
	// node connects to itself when told to (addnode).
	// src/sync/stages/pull/peer.rs in maestro-org/maestro-symphony;
	// src/crawl.rs in achow101/dnsseedrs; Satscraft/bip324-probe.
	// /your-fork:/ has no sentence of its own (see kinds.go). Bitcoin Lab's
	// record of the node this was written for, 13.9. to 25.10.2026: 241 sessions from 187 addresses, offering nothing, no
	// transactions, gone after 18 seconds at the median; the same handshake
	// as Bitnodes' crawler (version, sendaddrv2, verack, then getaddr). A
	// session that stays, like Bitnodes' own, asks for nothing: getpeerinfo
	// on 31.10.2026, one of 22 hours, no getaddr.
	{regexp.MustCompile(`^/symphony:`), "Maestro Symphony, a Bitcoin indexer and API server", "Maestro Symphony indexer", ""},
	{regexp.MustCompile(`^/crawlrs:`), "dnsseedrs, a DNS seeder by Ava Chow, a Bitcoin Core maintainer", "dnsseedrs (Ava Chow)", ""},
	{regexp.MustCompile(`^/bip324-probe:`), "bip324-probe, a tool that checks whether nodes offer encrypted v2 connections", "bip324-probe v2 checker", ""},
	{regexp.MustCompile(`^/p2p-observer:`), "p2p-observer, 0xB10C's tool that connects to nodes to watch the network", "0xB10C's p2p-observer", ""},
	{regexp.MustCompile(`^/p2p-extractor:`), "peer-observer's p2p extractor: nodes are told to connect to it, and it records what they send", "peer-observer's extractor", ""},
	{regexp.MustCompile(`^/AgenticBitcoin:`), "AgenticBitcoin, Bitcoin rewritten in Rust by AI agents", "AgenticBitcoin (AI-made)", ""},
	{regexp.MustCompile(`^/SatoshiSharp:`), "SatoshiSharp, parts of Bitcoin written in C#", "SatoshiSharp (C#)", ""},
	{regexp.MustCompile(`^NBXplorer-`), "NBXplorer, the indexer behind BTCPay Server", "NBXplorer (BTCPay)", ""},
	// NodeConstants.scala in bitcoin-s/bitcoin-s; indexer.rs in stacks-core.
	{regexp.MustCompile(`^/bitcoin-s:`), "bitcoin-s, a Bitcoin toolkit in Scala with a light client on compact block filters", "bitcoin-s toolkit (Scala)", ""},
	{regexp.MustCompile(`^Stacks/`), "A Stacks node: follows Bitcoin's blocks for the Stacks chain built on it", "Stacks node", ""},
	{regexp.MustCompile(`/utreexod:`), "utreexod, a Utreexo full node built on btcd", "utreexod Utreexo node", ""},
	{regexp.MustCompile(`^/python-bitcoinlib:`), "python-bitcoinlib, a Python library for scripts and tests", "python-bitcoinlib", ""},
	{regexp.MustCompile(`^/nakamoto:`), "nakamoto, a light client on compact block filters", "nakamoto light client", ""},
	{regexp.MustCompile(`(?i)/spvwallet:`), "spvwallet, the wallet of OpenBazaar, a marketplace closed in 2021", "OpenBazaar's spvwallet", ""},
	// Autarkysoft's C# toolkit (Configuration.cs); it pushes transactions
	// under "/Satoshi:0.22.0/" instead, so only its own name lands here.
	{regexp.MustCompile(`^/Denovo:`), "Denovo, a Bitcoin toolkit in C#: pushes transactions, mines on testnet", "Denovo toolkit (C#)", ""},
	// network.py in jimmysong/programmingbitcoin and the course exercises.
	{regexp.MustCompile(`^/programming(bitcoin|blockchain):`), "Code from Jimmy Song's book or course Programming Bitcoin", "Programming Bitcoin code", ""},
	{regexp.MustCompile(`^/bip324-ts:`), "bip324-ts, a TypeScript library for encrypted v2 connections", "bip324-ts library", ""},
	// dsn.kastel.kit.edu/bitcoin, the DSN group at KIT.
	{regexp.MustCompile(`(?i)^/dsn\.[a-z]+\.kit\.edu/`), "KIT Karlsruhe's Bitcoin network monitoring, measuring the network since 2015", "KIT network monitoring", "KIT"},
	// app/network/scanner.py in DogukanBaysal/DaeConn; the other two from
	// 0xB10C's list of 15.05.2023 (2023-05-inv-eaters in bitcoin-noc/banlist).
	{regexp.MustCompile(`^/connector/$`), "DaeConn, a research tool that measures the networks of Bitcoin and its forks", "DaeConn research tool", ""},
	{regexp.MustCompile(`^/nodes\.mom\.market:`), "nodes.mom.market, a site that monitors Bitcoin nodes (BitFence)", "mom.market monitoring", ""},
	{regexp.MustCompile(`^/btcwire:[^/]*/bitcoinstats\.com:`), "bitcoinstats.com, a site with statistics on the Bitcoin network", "bitcoinstats.com", ""},
	// btcd's wire library with the app name after it left empty.
	{regexp.MustCompile(`^/btcwire:[^/]*/:/$`), "A program on btcd's Go library that left its own name empty", "btcd library, empty name", ""},
	{regexp.MustCompile(`(?i)btcd:`), "btcd, a full node written in Go", "btcd node (Go)", ""},
	{regexp.MustCompile(`(?i)libbitcoin`), "libbitcoin, a full node written in C++, independent of Core", "libbitcoin node", ""},
	{regexp.MustCompile(`(?i)/bcoin:`), "bcoin, a full node written in JavaScript", "bcoin node (JS)", ""},
	{regexp.MustCompile(`(?i)gocoin`), "Gocoin, a full node and wallet written in Go", "Gocoin node and wallet", ""},
	// gosuda/bitcoin-rs, crates/primitives/src/version.rs: "/bitcoin-rs:<version>/".
	{regexp.MustCompile(`^/bitcoin-rs:`), "bitcoin-rs, a full node written in Rust, independent of Core", "bitcoin-rs node (Rust)", ""},
	// No public source. On the node this was written for, 28.10. to
	// 01.11.2026: 23 sessions from 16 addresses, all in Cloudflare's network
	// (AS13335), none sending BIP 331's sendpackages, gone after 11 s (median).
	// Only the name is said: what one node saw is no rule for all of them.
	{regexp.MustCompile(`^/cf-package-relay:`), "Named after package relay, which passes related transactions on together (BIP 331)", "Named after package relay", ""},
	// Bitcoin Cash clients carry its block size as "(EB8.0)" too, so they
	// come before Classic.
	{regexp.MustCompile(`(?i)Bitcoin ABC`), "Bitcoin ABC, a Bitcoin Cash client, eCash's node since 2020", "Bitcoin ABC (BCH)", ""},
	{regexp.MustCompile(`(?i)BUCash|BCHUnlimited`), "Bitcoin Unlimited, a Bitcoin Cash client", "Bitcoin Unlimited (BCH)", ""},
	{regexp.MustCompile(`(?i)Bitcoin SV`), "Bitcoin SV, the node of BSV, which split from Bitcoin Cash in 2018", "Bitcoin SV (BSV)", ""},
	{regexp.MustCompile(`(?i)Bitcoin XT`), "Bitcoin XT, a fork of Core for bigger blocks that went on to Bitcoin Cash", "Bitcoin XT", ""},
	{regexp.MustCompile(`(?i)SuperBitcoin`), "Super Bitcoin, a fork of Bitcoin from December 2017 with a chain of its own", "Super Bitcoin fork", ""},
	{regexp.MustCompile(`(?i)iriumd`), "Irium, an altcoin with a chain of its own", "Irium altcoin", ""},
	{regexp.MustCompile(`(?i)vds[_ ]`), "VDS, an altcoin with a chain of its own", "VDS altcoin", ""},
	{regexp.MustCompile(`(?i)Classic:|\(EB[0-9]`), "Bitcoin Classic: a Bitcoin Cash client", "Bitcoin Classic (BCH)", ""},
}

// fakeAbout says, for a disguised peer, what it claims and what it probably is. The
// claim comes from its user agent, the guess from what Core observed of it -
// the one part nobody can write into a string.
func fakeAbout(subver, walletAbout string, services []string) string {
	claim := "Bitcoin Core"
	if v := coreVersion.FindStringSubmatch(subver); v != nil {
		claim += " " + v[1]
	}
	wallet := walletAbout != ""
	if strings.HasPrefix(walletAbout, "bitcoinj,") {
		// The library without an app name claims bitcoinj, not Core or a wallet.
		claim = claimOf(subver)
	} else if wallet {
		claim, _, _ = strings.Cut(walletAbout, ",")
		claim = "the wallet " + claim
	}
	var guess string
	blocks, v2 := false, false
	for _, s := range services {
		blocks = blocks || s == "NETWORK"
		v2 = v2 || s == "P2P_V2"
	}
	switch {
	case wallet && blocks:
		guess = "a node or scanner behind an old wallet name - it offers blocks, which no wallet does"
	case wallet && v2:
		guess = "a node or scanner behind an old wallet name - it speaks v2, which no wallet of that age does"
	case misspeltCore(subver) != "":
		guess = "not Bitcoin Core - " + misspeltCore(subver)
		if services != nil && len(services) == 0 {
			guess += " and offers no services"
		}
	default:
		guess = "unknown software"
	}
	return "Claims: " + claim + "\nProbably: " + guess
}

// claimOf is the software a user agent names, readably: "/bcoin:v1.0.0/" is
// "bcoin v1.0.0", Core and Knots are spelled out.
func claimOf(subver string) string {
	if v := coreVersion.FindStringSubmatch(subver); v != nil {
		if strings.Contains(subver, "Knots:") {
			return "Bitcoin Knots (Core " + v[1] + ")"
		}
		return "Bitcoin Core " + v[1]
	}
	first, _, _ := strings.Cut(strings.Trim(subver, "/"), "/")
	return strings.Replace(first, ":", " ", 1)
}

// sessionLine is the line for a name none of the rules know, so no peer is left
// without one. Said as what we know, not as what it is: others may know it.
func sessionLine(services []string, relayTxes *bool, recv, sent map[string]int64, connTime int64, now time.Time) (line, short string, saysNoTx bool) {
	// A peer that has been connected for a while and asked for nothing is no
	// full node, whatever services it lists. Seen 21. to 26.10.2026: /your-fork:0.1/
	// and /alt-bitnodes:0.1/ staying for hours, hearing of transactions
	// and never fetching one, /pydroid-pool:3/ wanting neither.
	if quietAfterHandshake(recv, connTime, now) {
		if wantsTx(relayTxes) {
			// "Hears" only if we announced something to it.
			if sent["inv"] > 0 {
				return "Only listens: hears new transactions, fetches none", "Only listens", false
			}
			return "Only listens: asks for nothing", "Only listens", false
		}
		return "Only listens: wants no transactions, asks for nothing", "Only listens", true
	}
	// What it did, where Core counted it: a name nobody knows still says
	// little, its messages say more. Seen on the node this was written for,
	// 21. to 24.10.2026: /pwsh-pusher/ and /p2aburn/ pushing transactions
	// nobody asked for, others
	// fetching only headers, /Bitcoin:1.0.0/ asking for addresses each time,
	// /relay-geo:0.1/ fetching every transaction and announcing none.
	// A real node always offers a service, so one that offers none cannot be
	// the node reading below; nil means Core did not say.
	maybeNode := services == nil || len(services) > 0
	or := func(node, other string) string {
		if maybeNode {
			return node + ", or " + other
		}
		return other
	}
	switch {
	// BIP 35: "send me your whole mempool". Seen 22. to 26.10.2026 as /curl:8.5.0/,
	// twenty times from one address, each right after the handshake.
	case recv["mempool"] > 0:
		return "Asks for our mempool", "Asks for our mempool", false
	case recv["tx"] > 0 && recv["inv"] == 0:
		return "Pushes transactions without announcing them", "Pushes tx unannounced", false
	// Headers and no transactions is also what a real node does on a
	// connection for blocks only (2.2% of 2,553 Core and Knots sessions on
	// one node, 16. to 28.10.2026), so both readings are given. One that asks
	// for addresses too runs with blocksonly=1 if it is a node (noTxWhy).
	case refusesTx(relayTxes) && (recv["getheaders"] > 0 || recv["sendheaders"] > 0) && recv["getdata"] == 0 && recv["getaddr"] > 0:
		return "Asks for headers and addresses and wants no transactions: " + or("a node running blocksonly", "a program that tracks the chain"), "Headers, addresses only", true
	case refusesTx(relayTxes) && (recv["getheaders"] > 0 || recv["sendheaders"] > 0) && recv["getdata"] == 0:
		return "Asks for headers and wants no transactions: " + or("a block-only connection", "a program that tracks the chain"), "Asks only for headers", true
	// Asking for data alone is no sign of listening: a peer that fetches
	// blocks asks the same way. Only transactions sent to it count.
	case wantsTx(relayTxes) && recv["getdata"] > 0 && sent["tx"] > 0 && recv["inv"] == 0 && recv["tx"] == 0:
		// 0.5% of the real nodes above did the same: one that hears of
		// everything from others first has nothing to announce.
		return "Fetches transactions from your node and announces none: " + or("a node that hears of them elsewhere first", "a program collecting them"), "Takes tx, announces none", false
	case recv["getaddr"] > 0 && recv["getheaders"] == 0 && recv["getdata"] == 0:
		// 6.2% of the real nodes above did the same: one in sync asks for no headers.
		return "Asks for addresses, not for headers or blocks: " + or("a node already in sync", "a crawler"), "Asks only for addresses", false
	}
	// Offering blocks is only a guess at what it is, so what it did comes
	// first. Seen 04. to 06.11.2026: /spynode:0.1/ listing NETWORK, fetching
	// transactions for a day and a half and sending none back, was called a
	// full node.
	for _, s := range services {
		if s == "NETWORK" {
			return operatorNamedAbout, "Probably a full node", false
		}
	}
	return unknownAbout, "Unknown software", false
}

// otherAbout is sessionLine's sentence alone.
func otherAbout(services []string, relayTxes *bool, recv, sent map[string]int64, connTime int64, now time.Time) string {
	line, _, _ := sessionLine(services, relayTxes, recv, sent, connTime, now)
	return line
}

// The two lines of otherAbout that say nothing the peer did: a guess from
// its services, and no guess at all.
const (
	operatorNamedAbout = "Probably a full node under a name its operator chose"
	unknownAbout       = "Software with a name we don't recognise"
)

// observedPart is otherAbout's line without the guess at what such a peer
// is: under a sentence that already says what it is, only what it did
// belongs. "Only listens: asks for nothing" keeps what follows the colon,
// "Asks for headers ...: a program that tracks the chain" what comes before.
func observedPart(line string) string {
	if rest, ok := strings.CutPrefix(line, "Only listens: "); ok {
		return strings.ToUpper(rest[:1]) + rest[1:]
	}
	before, _, _ := strings.Cut(line, ": ")
	return before
}

// aboutRule is one line of aboutRules.
type aboutRule = struct {
	re          *regexp.Regexp
	text        string
	label, name string
}

// ruleOf is the first rule that knows the user agent.
func ruleOf(subver string) (aboutRule, bool) {
	for _, r := range aboutRules {
		if r.re.MatchString(subver) {
			return r, true
		}
	}
	return aboutRule{}, false
}

func aboutOf(subver string) string {
	r, _ := ruleOf(subver)
	return r.text
}

// noAgentLine is what a peer that sent its version but left its name empty
// did next, in a few words. Seen 21. to 26.10.2026, 429 such sessions: 113
// asked for headers and addresses like a full node, 43 asked for addresses
// and nothing else, the rest said nothing or only answered pings.
func noAgentLine(recv map[string]int64, connTime int64, now time.Time) (line, short string) {
	// The pill says it has no name; the sentence says what it did.
	switch {
	case recv["getheaders"] > 0 && recv["getaddr"] > 0:
		return "Asks for headers and addresses, like a full node does", "Acts like a full node"
	case recv["getheaders"] > 0 && recv["sendcmpct"] > 0:
		return "Asks for headers and compact blocks, like a full node does", "Acts like a full node"
	case recv["getaddr"] > 0 && recv["getheaders"] == 0 && recv["getdata"] == 0:
		return "Asks for addresses, not for headers or blocks", "Asks only for addresses"
	case quietAfterHandshake(recv, connTime, now):
		return "Introduced itself, then asked for nothing", "Asked for nothing"
	}
	return "Introduced itself without a name; nothing it did tells what it is", "Nothing it did tells"
}

// hemiUnnamed is how Hemi's Bitcoin code (tbc) names itself when the program
// built on it sets no name: "/%v:%v/" with the component empty, the version
// Major.Minor.Patch-dev+ the first nine characters of the commit, and Core
// drops the "+" (rawpeer.go, version.go in github.com/hemilabs/heminetwork).
// The commits seen (539f4e33c, 923cba97a, c1771608d) are not in Hemi's public
// repository, so it is "probably".
var hemiUnnamed = regexp.MustCompile(`^/:[0-9]+\.[0-9]+\.[0-9]+-dev\+?[0-9a-f]{9}/$`)

const hemiUnnamedAbout = "Probably a program on Hemi's Bitcoin code (tbc) that left its own name empty"

// noAgentAbout is noAgentLine's sentence alone.
func noAgentAbout(recv map[string]int64, connTime int64, now time.Time) string {
	line, _ := noAgentLine(recv, connTime, now)
	return line
}
