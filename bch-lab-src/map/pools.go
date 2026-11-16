package main

import (
	"net/netip"
	"regexp"
)

// Which mining pool a peer belongs to. A pool's node rarely says so in its
// user agent: ckpool's block relay calls itself /ckp2p:2.0/ for every pool
// that runs it, and a pool's own bitcoind is a plain /Satoshi:/. So the pool
// is named here only where the address is surely the pool's: it handed out
// mining jobs carrying the pool's coinbase tag to a stratum subscribe (the
// tag in brackets), or it is what a stratum host name from the pool's own
// documentation resolves to, or the pool published it as its node. It need
// not have connected to any node yet. Left out: addresses shared with others
// (Cloudflare, anycast front doors, one address for two pools), and IPv6 -
// inbound IPv6 peers reach Core through Docker under one hidden address, so
// only IPv4 is listed. Peers of a pool that are not on this list stay
// "Pool node" without a name: a pool moves its servers.
type poolNode struct {
	pool, proof string
}

var poolNodes = map[netip.Addr]poolNode{
	// 31.10.2026: forward and reverse DNS agree (uwsolo.ckpool.org), an
	// official Solo CK stratum endpoint.
	netip.MustParseAddr("147.135.104.8"): {"Solo CK", "is uwsolo.ckpool.org, an official Solo CK stratum endpoint"},
	// 01.11.2026: europe.mining-dutch.nl resolves here, and the pool's own BTC
	// page lists it as the Europe stratum host (port 3465). Its blocks carry
	// /Mining-Dutch/, e.g. 964,388.
	netip.MustParseAddr("51.178.76.133"): {"Mining-Dutch", "is europe.mining-dutch.nl, Mining-Dutch's BTC stratum host"},
	// 01.11.2026: btc.zsolo.bid resolves here, and zSolo's own BTC page lists it
	// as the BTC stratum host (ports 6057, 6060; archived 21.03.2026). It joined
	// this node as /Satoshi:29.2.0/ on 30.10.2026. A solo pool; no block known yet.
	netip.MustParseAddr("92.119.126.14"): {"zSolo", "is btc.zsolo.bid, zSolo's BTC stratum host"},
	// AtlasPool runs ckpool at points of presence on Amazon. The tag names its
	// own site, not where the server is: the one in London says FRA.
	netip.MustParseAddr("3.11.221.101"):  {"AtlasPool", "hands out AtlasPool's mining jobs (AtlasPool/FRA)"},
	netip.MustParseAddr("63.184.166.44"): {"AtlasPool", "hands out AtlasPool's mining jobs (AtlasPool/FRA)"},
	netip.MustParseAddr("3.228.156.48"):  {"AtlasPool", "hands out AtlasPool's mining jobs (AtlasPool/IAD)"},
	netip.MustParseAddr("32.189.241.52"): {"AtlasPool", "hands out AtlasPool's mining jobs (AtlasPool/PDX)"},
	// ckpool tagged /pool.mutatrum.com/ on 3333, Public-Pool on 3334.
	netip.MustParseAddr("95.168.173.100"): {"mutatrum", "hands out mutatrum's mining jobs (/pool.mutatrum.com/)"},
	netip.MustParseAddr("66.85.130.202"):  {"KanoPool", "is stratum.kano.is, and hands out KanoPool's mining jobs"},
	netip.MustParseAddr("62.2.188.226"):   {"Blitzpool", "is blitzpool.yourdevice.ch, and hands out Blitzpool's mining jobs"},
	netip.MustParseAddr("179.27.118.130"): {"PyBLOCK", "is pool.pyblock.xyz"},
	netip.MustParseAddr("144.202.96.186"): {"MintSolo", "hands out MintSolo's mining jobs (MintSolo GSS)"},
	netip.MustParseAddr("149.28.149.108"): {"MintSolo", "hands out MintSolo's mining jobs (MintSolo GSS)"},
	// Says (xbtpool.io); xbtpool is ctrlpool now, and its jobs on 4334 are tagged CTRL.
	netip.MustParseAddr("161.97.128.46"): {"ctrlpool", "hands out ctrlpool's mining jobs (CTRL), formerly xbtpool"},
	// WarpPool's peering page names this its reference pool's Bitcoin Core
	// node (Nuremberg, inbound open) and asks nodes to addnode it.
	netip.MustParseAddr("91.98.76.244"): {"WarpPool", "is the node WarpPool publishes for peering"},
	// 01.11.2026: the stratum host names in the pools' own setup pages, and
	// what they resolved to that day. Mineshop Solo, SoloPool.com and the
	// 256 Foundation's test pool had joined this node from them before.
	netip.MustParseAddr("38.134.40.199"):   {"1Miner", "is 1miner.net, 1Miner's stratum host"},
	netip.MustParseAddr("34.196.36.85"):    {"256 Foundation", "is pool.256foundation.org, 256 Foundation's stratum host"},
	netip.MustParseAddr("186.240.148.122"): {"Bitaxe Pool", "is stratum.bitaxe.de, Bitaxe Pool's stratum host"},
	netip.MustParseAddr("77.42.66.235"):    {"Bitronics", "is pool.bitronics.store, Bitronics' stratum host"},
	netip.MustParseAddr("144.76.168.26"):   {"DEMAND", "is mining.dmnd.work, DEMAND's stratum host"},
	netip.MustParseAddr("93.115.29.13"):    {"DirectPool", "is stratum.directpool.io, DirectPool's stratum host"},
	netip.MustParseAddr("81.17.154.237"):   {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("91.210.170.31"):   {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("92.255.77.246"):   {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("130.193.56.79"):   {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("147.45.185.147"):  {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("161.104.58.162"):  {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("185.161.64.42"):   {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("185.161.64.44"):   {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("188.225.74.178"):  {"EMCD", "is kz.emcd.network and gate.emcd.network, EMCD's stratum host"},
	netip.MustParseAddr("3.22.184.252"):    {"Foundry USA", "is btc.foundryusapool.com, Foundry USA's stratum host"},
	netip.MustParseAddr("3.148.75.19"):     {"Foundry USA", "is btc.foundryusapool.com, Foundry USA's stratum host"},
	netip.MustParseAddr("3.149.136.102"):   {"Foundry USA", "is btc.foundryusapool.com, Foundry USA's stratum host"},
	netip.MustParseAddr("5.180.174.162"):   {"Kryptex", "is btc-ru.kryptex.network, Kryptex's stratum host"},
	netip.MustParseAddr("142.132.135.164"): {"Kryptex", "is btc.kryptex.network and other Kryptex hosts, Kryptex's stratum host"},
	netip.MustParseAddr("158.255.5.154"):   {"Kryptex", "is btc-ru.kryptex.network, Kryptex's stratum host"},
	netip.MustParseAddr("158.255.5.205"):   {"Kryptex", "is btc-ru.kryptex.network, Kryptex's stratum host"},
	netip.MustParseAddr("209.38.2.206"):    {"Lincoin", "is na.lincoin.com, Lincoin's stratum host"},
	netip.MustParseAddr("188.245.242.25"):  {"Mineshop Solo", "is stratum-de.solo.mineshop.eu, Mineshop Solo's stratum host"},
	netip.MustParseAddr("204.168.216.218"): {"Mineshop Solo", "is solo.mineshop.eu, Mineshop Solo's stratum host"},
	netip.MustParseAddr("172.104.230.135"): {"Nerdminer.de", "is pool.nerdminer.de, Nerdminer.de's stratum host"},
	netip.MustParseAddr("188.245.187.14"):  {"Noderunners", "is pool.noderunners.network, Noderunners's stratum host"},
	netip.MustParseAddr("20.157.66.196"):   {"OCEAN", "is mine.ocean.xyz, OCEAN's stratum host"},
	netip.MustParseAddr("159.203.151.36"):  {"OCEAN", "is default.mine.ocean.xyz, OCEAN's stratum host"},
	netip.MustParseAddr("213.136.77.252"):  {"Parasite", "is parasite.wtf, Parasite's stratum host"},
	netip.MustParseAddr("138.68.91.126"):   {"Power Mining", "is pool.powermining.io, Power Mining's stratum host"},
	netip.MustParseAddr("207.244.242.7"):   {"PowerPool", "is btc.us.stratum.solo.powerpool.io, PowerPool's stratum host"},
	netip.MustParseAddr("38.51.144.232"):   {"Public Pool", "is public-pool.io, Public Pool's stratum host"},
	netip.MustParseAddr("165.154.215.49"):  {"SECPOOL", "is ru.secpool.com and ru2.secpool.com, SECPOOL's stratum host"},
	netip.MustParseAddr("165.154.215.213"): {"SECPOOL", "is ru.secpool.com and ru2.secpool.com, SECPOOL's stratum host"},
	netip.MustParseAddr("15.204.102.129"):  {"Solo CK", "is solo.ckpool.org, Solo CK's stratum host"},
	netip.MustParseAddr("49.255.179.152"):  {"Solo CK", "is ausolo.ckpool.org, Solo CK's stratum host"},
	netip.MustParseAddr("51.79.162.31"):    {"Solo CK", "is sgsolo.ckpool.org, Solo CK's stratum host"},
	netip.MustParseAddr("51.81.56.15"):     {"Solo CK", "is uesolo.ckpool.org, Solo CK's stratum host"},
	netip.MustParseAddr("57.129.83.51"):    {"Solo CK", "is eusolo.ckpool.org and stratum.ckpool.org, Solo CK's stratum host"},
	netip.MustParseAddr("45.32.183.77"):    {"SoloFury", "is uk-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("45.76.82.121"):    {"SoloFury", "is eu-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("45.76.187.0"):     {"SoloFury", "is asia-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("64.177.70.240"):   {"SoloFury", "is me-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("137.220.41.11"):   {"SoloFury", "is pnw-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("139.84.247.214"):  {"SoloFury", "is afr-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("144.202.22.193"):  {"SoloFury", "is btc.solofury.com and us-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("202.182.101.87"):  {"SoloFury", "is jp-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("216.238.100.215"): {"SoloFury", "is lat-btc.solofury.com, SoloFury's stratum host"},
	netip.MustParseAddr("148.230.98.87"):   {"SoloLuck", "is stratum.sololuck.io and sea.stratum.sololuck.io, SoloLuck's stratum host"},
	netip.MustParseAddr("161.118.229.240"): {"SoloLuck", "is sg.stratum.sololuck.io, SoloLuck's stratum host"},
	netip.MustParseAddr("212.83.157.26"):   {"SoloMining.de", "is pool.solomining.de, SoloMining.de's stratum host"},
	netip.MustParseAddr("23.118.217.146"):  {"SoloMiningHQ", "is btc.solomininghq.com, SoloMiningHQ's stratum host"},
	netip.MustParseAddr("15.204.198.148"):  {"SoloPool.com", "is east.solopool.com, SoloPool.com's stratum host"},
	netip.MustParseAddr("38.60.209.246"):   {"SoloPool.com", "is sp.solopool.com, SoloPool.com's stratum host"},
	netip.MustParseAddr("51.79.228.213"):   {"SoloPool.com", "is asia.solopool.com, SoloPool.com's stratum host"},
	netip.MustParseAddr("130.94.45.4"):     {"SoloPool.com", "is me.solopool.com, SoloPool.com's stratum host"},
	netip.MustParseAddr("141.95.47.237"):   {"SoloPool.com", "is eu.solopool.com, SoloPool.com's stratum host"},
	netip.MustParseAddr("148.135.34.243"):  {"SoloPool.com", "is la.solopool.com, SoloPool.com's stratum host"},
	netip.MustParseAddr("3.64.120.210"):    {"SpiderPool", "is btc-eu.spiderpool.com, SpiderPool's stratum host"},
	netip.MustParseAddr("50.18.58.32"):     {"SpiderPool", "is btc-us.spiderpool.com, SpiderPool's stratum host"},
	netip.MustParseAddr("52.28.105.80"):    {"SpiderPool", "is btc-eu.spiderpool.com, SpiderPool's stratum host"},
	netip.MustParseAddr("54.241.30.190"):   {"SpiderPool", "is btc-us.spiderpool.com, SpiderPool's stratum host"},
	netip.MustParseAddr("63.181.206.8"):    {"SpiderPool", "is btc-eu.spiderpool.com, SpiderPool's stratum host"},
	netip.MustParseAddr("190.92.247.97"):   {"SpiderPool", "is btc-as.spiderpool.com, SpiderPool's stratum host"},
	netip.MustParseAddr("139.162.137.20"):  {"Sunnydecree", "is pool.sunnydecree.de, Sunnydecree's stratum host"},
	netip.MustParseAddr("18.157.247.222"):  {"ViaBTC", "is btc-ssl.powhashing.com, ViaBTC's stratum host"},
	netip.MustParseAddr("18.163.83.212"):   {"ViaBTC", "is btc-ssl.powhashing.com, ViaBTC's stratum host"},
	netip.MustParseAddr("158.160.181.81"):  {"ViaBTC", "is btc-ssl.powhashing.com, ViaBTC's stratum host"},
}

// Block relay networks: not pools, but what they send a node is new blocks,
// fast, and nothing else. Their peers name nothing (no user agent at all).
var relayNodes = map[netip.Addr]poolNode{
	// WarpPool's peering page lists M45Core.com:8333 as BTCFlux's relay. On
	// 23.10.2026 it came in 17 times, every five minutes, without a user agent,
	// for compact blocks only.
	netip.MustParseAddr("64.20.34.46"): {"M45Core", "is M45Core.com, BTCFlux's block relay"},
}

// relayOf says which block relay network an address belongs to.
func relayOf(addr string) (poolNode, bool) {
	if a, ok := ipOf(addr); ok {
		n, ok := relayNodes[a]
		return n, ok
	}
	return poolNode{}, false
}

// ViaBTC's own relay software, bitpeer, introduces itself as an old Bitcoin
// Core: /Satoshi:0.19.0.1/, protocol 70015, and of all services only
// WITNESS. 0xB10C traced it to ViaBTC's code and its block broadcasts
// (b10c.me, March 2024). A real Core 0.19 offers NETWORK_LIMITED at least,
// so it does not match. On one node three of them, all on Amazon in
// Europe, came daily from 16.10.2026, sent blocks and no transactions, and
// were never first in 3,192 blocks.
const viaBitpeerAbout = "Claims: Bitcoin Core 0.19.0.1\nProbably: ViaBTC's own relay software (bitpeer) – it says Bitcoin Core 0.19.0.1, but offers only WITNESS"

func isViaBitpeer(subver string, version int, services []string) bool {
	return subver == "/Satoshi:0.19.0.1/" && version == 70015 && len(services) == 1 && services[0] == "WITNESS"
}

// A pool the user agent names itself, where no address says more.
var poolLabels = []struct {
	re   *regexp.Regexp
	pool string
}{
	{regexp.MustCompile(`(?i)\(PyBLOCK-POOL\)`), "PyBLOCK"},
	{regexp.MustCompile(`(?i)\(xbtpool\.io\)`), "ctrlpool"},
	{regexp.MustCompile(`(?i)^/pydroid-pool:`), "pydroid"},
}

// poolOf says which pool a peer belongs to, and how that is known: from the
// list of proven addresses, else from a pool the user agent names. The second
// result says the address proved it.
func poolOf(addr, subver string) (poolNode, bool) {
	if a, ok := ipOf(addr); ok {
		if n, ok := poolNodes[a]; ok {
			return n, true
		}
	}
	for _, l := range poolLabels {
		if l.re.MatchString(subver) {
			return poolNode{pool: l.pool}, false
		}
	}
	return poolNode{}, false
}
