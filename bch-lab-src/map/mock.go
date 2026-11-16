package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/benunskilled/peer-map/asn"
	"github.com/benunskilled/peer-map/geo"
)

// mockPeers returns plausible getpeerinfo output for PEERMAP_MOCK=1, so the
// dashboard can be tried - and screenshotted - without a node.
//
// Every address is from the documentation ranges (RFC 5737 for IPv4, RFC 3849
// for IPv6), never a real host, so a store screenshot cannot show somebody's
// server. Those ranges have no location by definition, so demo peers carry
// their location directly; the real lookup is covered by the tests instead.
// The dashboard shows a "demo data" banner whenever this is in use.
func mockPeers(ctx context.Context) ([]rawPeer, error) {
	// Fixed seed: the same demo node every refresh, with a few inbound peers
	// swapped out each interval so the refresh is visible.
	r := rand.New(rand.NewSource(42))
	churn := rand.New(rand.NewSource(time.Now().Unix() / int64(interval/time.Second)))
	now := time.Now().Unix()

	// Weighted roughly like a real node's peer list: heavy on Germany, the US
	// and the big hosting countries, with several regions each.
	//
	// Each place carries the operator a machine there plausibly sits with, and
	// every number and name below is the one the ASN table itself holds. Three
	// of them repeat across countries on purpose - Amazon in Frankfurt,
	// Virginia and Singapore, Hetzner in Saxony and Helsinki - because that is
	// the case the column exists for: different flags, one company.
	op := func(number uint32, name string) asn.Info { return asn.Info{Number: number, Name: name} }
	places := []struct {
		cc, region string
		net        asn.Info
	}{
		{"DE", "Hesse", op(16509, "Amazon.com, Inc.")},
		{"DE", "Bavaria", op(24940, "Hetzner Online GmbH")},
		{"DE", "Berlin", op(3320, "Deutsche Telekom AG")},
		{"DE", "North Rhine-Westphalia", op(8560, "IONOS SE")},
		{"DE", "Saxony", op(24940, "Hetzner Online GmbH")},
		{"US", "Virginia", op(16509, "Amazon.com, Inc.")},
		{"US", "California", op(15169, "Google LLC")},
		{"US", "Oregon", op(20473, "The Constant Company, LLC")},
		{"US", "New York", op(14061, "DigitalOcean, LLC")},
		{"US", "Texas", op(7922, "Comcast Cable Communications, LLC")},
		{"NL", "North Holland", op(60781, "LeaseWeb Netherlands B.V.")},
		{"NL", "North Holland", op(1136, "KPN B.V.")},
		{"FR", "Ile-de-France", op(12876, "Scaleway SAS")},
		{"FI", "Uusimaa", op(24940, "Hetzner Online GmbH")},
		{"GB", "England", op(2856, "British Telecommunications PLC")},
		{"CA", "Quebec", op(16276, "OVH SAS")},
		{"CA", "Ontario", op(812, "Rogers Communications Canada Inc.")},
		{"CH", "Zurich", op(13030, "Init7 (Switzerland) Ltd.")},
		{"AT", "Vienna", op(8447, "A1 Telekom Austria AG")},
		{"SE", "Stockholm", op(3301, "Telia Company AB")},
		{"PL", "Mazovia", op(5617, "Orange Polska Spolka Akcyjna")},
		{"CZ", "Prague", op(5610, "O2 Czech Republic, a.s.")},
		{"ES", "Madrid", op(3352, "TELEFONICA DE ESPANA S.A.U.")},
		{"IT", "Lombardy", op(3269, "Telecom Italia S.p.A.")},
		{"JP", "Tokyo", op(2516, "KDDI CORPORATION")},
		{"SG", "", op(16509, "Amazon.com, Inc.")},
		{"HK", "Kowloon", op(4760, "PCCW IMS Limited")},
		{"AU", "New South Wales", op(1221, "Telstra Limited")},
		{"AU", "Victoria", op(9009, "M247 Europe SRL")},
		{"AU", "Queensland", op(1221, "Telstra Limited")},
		{"AU", "Western Australia", op(1221, "Telstra Limited")},
		{"BR", "Sao Paulo", op(28573, "Claro NXT Telecomunicacoes Ltda")},
		{"ZA", "Gauteng", op(37457, "Telkom SA Ltd.")},
		{"IN", "Maharashtra", op(55836, "Reliance Jio Infocomm Limited")},
	}
	locs := make([]geo.Location, 0, len(places))
	nets := make([]asn.Info, 0, len(places))
	for _, pl := range places {
		if loc, ok := geo.FindRegion(pl.cc, pl.region); ok {
			locs = append(locs, loc)
		} else {
			locs = append(locs, geo.Location{Country: pl.cc})
		}
		nets = append(nets, pl.net)
	}
	nodeServices := []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}
	// A 2016 wallet name that offers blocks and v2 - see fakeWalletKind.
	const fakeWallet = "/bitcoinj:0.14.5/Bitcoin Wallet:5.42/"
	// A real node hears from more than Bitcoin Core. The spread here is taken
	// from one evening on an actual node, so the demo shows the Kind column
	// doing its job instead of a wall of "Core".
	clients := []string{
		"/Satoshi:31.1.0/", "/Satoshi:31.1.0/", "/Satoshi:31.1.0/", "/Satoshi:30.0.0/",
		"/Satoshi:29.0.0/", "/Satoshi:28.1.0/", "/Satoshi:29.3.0/Knots:20260507/",
		"/bitcoinj:0.16.2/Bitcoin Wallet:9.26/", "/breadwallet:1.3.5/",
		"/btcwire:0.5.0/neutrino:0.17.1/", "/electrs:0.11.1/", "/Metrika-Bitnodes:0.1/",
		"/dsn.tm.kit.edu/bitcoin:0.9.99/", "/ckp2p:2.0/", "/Floresta:0.9.1/",
		fakeWallet, "/Sat0shi:31.0.0/", "/bitcoin-seeder:0.01/", "/Satoshi:0.14.2/", "",
		"/Satoshi:29.3.0/Knots:20260508/", "/Satoshi:29.4.2/Knots:20260508/",
	}

	nodeClients := []string{}
	for _, c := range clients {
		// Fakes arrive; on the node this was written for, none was ever dialled.
		if relaysBlocks(c) && kindOf(c) != fakeWalletKind && c != "" && anachronism(c, nodeServices) == "" {
			nodeClients = append(nodeClients, c)
		}
	}

	var out []rawPeer
	id := int64(1)
	v4, v6 := 0, 0
	addr := func(rr *rand.Rand, port int) (string, string) {
		if rr.Intn(4) == 0 {
			v6++
			return fmt.Sprintf("[2001:db8:%x::%x]:%d", rr.Intn(0xffff), v6, port), "ipv6"
		}
		v4++
		nets := []string{"192.0.2", "198.51.100", "203.0.113"}
		return fmt.Sprintf("%s.%d:%d", nets[v4%3], 1+(v4*37)%253, port), "ipv4"
	}
	add := func(typ string, inbound bool, network, a string, loc *geo.Location, net *asn.Info) {
		ping := 0.02 + r.Float64()*0.3
		// The lowest ping Core ever saw sits below the current one, the way it
		// does on a real node: the current value carries whatever was queued.
		minPing := ping * (0.55 + r.Float64()*0.35)
		// Only inbound gets the full spread. You never dial OUT to a phone
		// wallet or a crawler - an outbound or manual connection is one this
		// node chose, and it chooses nodes.
		pool := clients
		if !inbound {
			pool = nodeClients
		}
		subver := pool[r.Intn(len(pool))]
		// The observed flags follow the software rather than a coin toss,
		// because that is how they behave on a real node: a wallet or a
		// crawler offers nothing and Core never learns its chain, while a
		// full node does both. The rare last case is the interesting one -
		// something calling itself Core that behaves like neither.
		services, relay, headers := nodeServices, true, int64(967000+r.Intn(400))
		// A block-relay-only connection carries no transactions, by design.
		if typ == "block-relay-only" {
			relay = false
		}
		switch kindOf(subver) {
		case "Wallet", "Lightning wallet", "Light client", "Crawler", "Research scanner", "Indexer":
			services, headers = []string{}, -1
			relay = r.Intn(3) > 0
			if subver == fakeWallet {
				services = []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}
			}
		case "Node (Knots)":
			// Knots builds that offer the bits of other rules (forkOf).
			switch subver {
			case "/Satoshi:29.3.0/Knots:20260508/":
				services = append(append([]string{}, nodeServices...), reducedDataService)
			case "/Satoshi:29.4.2/Knots:20260508/":
				services = append(append([]string{}, nodeServices...), reducedDataService, blake2bService)
			}
		case "Node (Core)":
			if r.Intn(20) == 0 {
				services, relay, headers = []string{}, false, -1
			}
		}
		out = append(out, rawPeer{
			ID: id, Addr: a, Network: network, ConnectionType: typ, Inbound: inbound,
			Subver: subver, PingTime: &ping, MinPing: &minPing,
			ConnTime: now - int64(r.Intn(86400*3)), Transport: []string{"v1", "v2"}[r.Intn(2)],
			BytesSent: int64(r.Intn(50 << 20)), BytesRecv: int64(r.Intn(200 << 20)),
			ServicesNames: services, RelayTxes: &relay, SyncedHeaders: &headers,
			mockLoc: loc, mockASN: net, mockHome: demoHome(id),
		})
		id++
	}
	public := func(rr *rand.Rand, typ string, inbound bool) {
		port := 8333
		if inbound {
			port = 40000 + rr.Intn(20000)
		}
		a, n := addr(rr, port)
		i := rr.Intn(len(locs))
		loc, net := locs[i], nets[i]
		add(typ, inbound, n, a, &loc, &net)
	}

	for i := 0; i < 8; i++ {
		public(r, "manual", false)
	}
	for i := 0; i < 10; i++ {
		public(r, "outbound-full-relay", false)
	}
	public(r, "block-relay-only", false)
	add("block-relay-only", false, "onion", "vww6ybal4bd7szmgncyruucpgfkqahzddi37ktceo3ah7ngmcopnpyyd.onion:8333", nil, nil)
	for i := 0; i < 22; i++ {
		src := r
		if i >= 19 {
			src = churn
		}
		public(src, "inbound", true)
	}
	add("inbound", true, "not_publicly_routable", "10.21.0.1:51234", nil, nil)
	add("inbound", true, "not_publicly_routable", "10.21.0.1:51980", nil, nil)
	// Two LinkingLion connections, so the header's LinkingLion count shows in
	// the demo too. Documentation addresses like everything here, marked by
	// hand: the real ranges would put real addresses into screenshots.
	add("inbound", true, "ipv4", "198.51.100.231:41532", &locs[2], &nets[2])
	out[len(out)-1].mockLinkingLion = true
	add("inbound", true, "ipv4", "198.51.100.232:50210", &locs[2], &nets[2])
	out[len(out)-1].mockLinkingLion = true
	add("inbound", true, "onion", "127.0.0.1:50122", nil, nil)
	add("inbound", true, "i2p", "ukeu3k5oycgaauneqgtnvselmt4yemvoilkln7jpvamvfx7dnkdq.b32.i2p:0", nil, nil)
	add("feeler", false, "ipv4", "192.0.2.250:8333", &locs[0], &nets[0]) // must be hidden
	return out, nil
}

// demoHome places the demo node itself in Hesse, Germany, on the first demo
// peer only - homeOf takes the first one it finds.
func demoHome(id int64) *geo.Location {
	if id != 1 {
		return nil
	}
	return &geo.Location{Country: "DE", Region: "Hesse", Lat: 50.11, Lon: 8.68}
}
