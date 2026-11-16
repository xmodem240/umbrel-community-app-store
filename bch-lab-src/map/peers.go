package main

import (
	"math"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/benunskilled/peer-map/asn"
	"github.com/benunskilled/peer-map/geo"
	"github.com/benunskilled/peer-map/torexit"
)

// rawPeer is the part of Bitcoin Core's getpeerinfo this app reads.
// connection_type and network exist since Core 0.21; transport_protocol_type
// since 26.0 (absent on older nodes, which is fine - it is display only).
type rawPeer struct {
	ID             int64    `json:"id"`
	Addr           string   `json:"addr"`
	Network        string   `json:"network"`
	ConnectionType string   `json:"connection_type"`
	Inbound        bool     `json:"inbound"`
	Subver         string   `json:"subver"`
	PingTime       *float64 `json:"pingtime"`
	MinPing        *float64 `json:"minping"`
	ConnTime       int64    `json:"conntime"`
	Transport      string   `json:"transport_protocol_type"`
	HBTo           bool     `json:"bip152_hb_to"`
	HBFrom         bool     `json:"bip152_hb_from"`
	BytesSent      int64    `json:"bytessent"`
	BytesRecv      int64    `json:"bytesrecv"`
	// What the peer offers and whether Core knows its chain. Pointers and a
	// nil slice on purpose: "Core did not report this field" and "the peer
	// offers nothing" are different answers and must not collapse into one.
	ServicesNames []string `json:"servicesnames"`
	RelayTxes     *bool    `json:"relaytxes"`
	// The fee rate below which the peer wants no transactions announced, in
	// BTC/kvB: what its last feefilter message said.
	MinFeeFilter *float64 `json:"minfeefilter"`
	// The protocol version from the peer's version message (Core 0.21 and
	// later send 70016). Zero when Core did not report it.
	Version       int    `json:"version"`
	SyncedHeaders *int64 `json:"synced_headers"`
	// The last block this node and the peer share; below zero while unknown.
	SyncedBlocks *int64 `json:"synced_blocks"`
	// Bytes received per message type. Read only to count pings and address
	// requests (see behaviourAbout); nil when Core did not report it.
	BytesRecvPerMsg map[string]int64 `json:"bytesrecv_per_msg"`
	// Bytes sent per message type. Read only to tell whether a peer that
	// asked for data was given transactions or blocks (see otherAbout).
	BytesSentPerMsg map[string]int64 `json:"bytessent_per_msg"`
	// The address this peer sees us under. Read only to place this node on
	// the map (see homeOf); never passed on.
	AddrLocal string `json:"addrlocal"`

	mockLoc         *geo.Location // demo data only: documentation addresses have no location
	mockASN         *asn.Info     // demo data only, same reason
	mockLinkingLion bool          // demo only: a documentation address standing in for LinkingLion
	mockHome        *geo.Location // demo only: where the demo node itself is
}

// Home is where this node is, as the map draws it: a region, never an address.
type Home struct {
	Country string  `json:"cc"`
	Region  string  `json:"region,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// homeOf places this node by the address its peers see it under: getpeerinfo's
// addrlocal. No extra RPC and nothing sent anywhere - the lookup is the
// bundled database, and only the region leaves this function. Nil when no
// peer reports an address that can be placed (a node reachable only over Tor,
// say).
//
// The vote is per region, not per address: IPv6 privacy addresses would
// otherwise split it, and an address that cannot be placed (CGNAT, a
// documentation range) simply does not count. Only connections this node
// opened vote. addrlocal is whatever the peer claims, and inbound peers are
// anyone - a crowd of them could otherwise move this node across the map.
// Inbound ones are asked only when there are no others.
func homeOf(raw []rawPeer) *Home {
	for _, p := range raw {
		if p.mockHome != nil {
			l := *p.mockHome
			return &Home{Country: l.Country, Region: l.Region, Lat: l.Lat, Lon: l.Lon}
		}
	}
	vote := func(inbound bool) *Home {
		count := map[uint16]int{}
		where := map[uint16]geo.Location{}
		var best uint16
		found := false
		for _, p := range raw {
			if p.Inbound != inbound || p.AddrLocal == "" {
				continue
			}
			a, ok := ipOf(p.AddrLocal)
			if !ok {
				continue
			}
			if !a.IsGlobalUnicast() || a.IsPrivate() {
				continue
			}
			l, ok := geo.Lookup(a)
			if !ok {
				continue
			}
			count[l.ID]++
			where[l.ID] = l
			if !found || count[l.ID] > count[best] {
				best, found = l.ID, true
			}
		}
		if !found {
			return nil
		}
		l := where[best]
		return &Home{Country: l.Country, Region: l.Region, Lat: l.Lat, Lon: l.Lon}
	}
	if h := vote(false); h != nil {
		return h
	}
	return vote(true)
}

// Peer is what the dashboard receives.
type Peer struct {
	ID       int64   `json:"id"`
	Addr     string  `json:"addr"`
	Network  string  `json:"network"`
	Group    string  `json:"group"` // manual | inbound | outbound
	Type     string  `json:"type"`  // Core's connection_type, verbatim
	Country  string  `json:"cc"`    // "" when it cannot be placed
	Region   string  `json:"region,omitempty"`
	RegionID uint16  `json:"rid,omitempty"` // groups peers per region; stable within one build
	Lat      float64 `json:"lat,omitempty"`
	Lon      float64 `json:"lon,omitempty"`
	// Where a peer is and whose machine it is are two questions. The second is
	// the one that catches three peers in three countries that all sit with the
	// same provider. Empty for an address no network announces.
	ASN      uint32 `json:"asn,omitempty"`
	Operator string `json:"operator,omitempty"`
	// The address is a Tor exit's: location and operator are the exit's, the
	// peer itself is somewhere behind it.
	TorExit bool `json:"tor_exit,omitempty"`
	// The name of the app when the peer is one of Umbrel's own, such as electrs.
	App string `json:"app,omitempty"`
	// The mining pool a pool node belongs to, where that is known (pools.go).
	Pool string `json:"pool,omitempty"`
	// How the address was proven the pool's, or a block relay's: for the panel, not the list.
	PoolProof string `json:"pool_proof,omitempty"`
	Subver    string `json:"subver"`
	Kind      string `json:"kind"`            // what the user agent claims; see kinds.go
	About     string `json:"about,omitempty"` // what that software is, when known
	// The about cut to one line, for the list, where the about is a sentence
	// from the rules or a session line with a guess after it.
	Label string `json:"label,omitempty"`
	// What the peer did in this session, where the about says what its name
	// stands for: the panel shows it under the about, the list does not.
	Seen string `json:"seen,omitempty"`
	// The chip the peer is counted under where its user agent does not say:
	// one program under many names (RESET), a borrowed name (Wasabi), or who
	// runs a scanner.
	Name string `json:"name,omitempty"`
	// Where the name is false or says nothing (Disguised, Other, No user
	// agent), what the peer does in a few words: the list shows it under the
	// pill, the panel the whole sentence (tellOf, behaviourTell).
	Tell    string `json:"tell,omitempty"`
	Why     string `json:"why,omitempty"`      // the whole sentence of the reason Tell leads with
	NoRelay bool   `json:"no_relay,omitempty"` // software that passes no blocks on
	// LinkingLion: from the ranges the bnoc banlist names, whatever kind the
	// user agent made it. The header counts these separately.
	LinkingLion bool `json:"linking_lion,omitempty"`
	// Observed rather than claimed, and omitted when false so the common peer
	// carries none of them.
	NoServices   bool   `json:"no_services,omitempty"`   // advertises NODE_NONE
	NoTxRelay    bool   `json:"no_tx_relay,omitempty"`   // version said: send me no transactions
	NoTxWhy      string `json:"no_tx_why,omitempty"`     // why, where it can be told
	NoTxSaid     bool   `json:"no_tx_said,omitempty"`    // the about already says it wants no transactions
	ChainUnknown bool   `json:"chain_unknown,omitempty"` // Core has never seen a header from it
	Fork         string `json:"fork,omitempty"`          // other consensus rules it offers to enforce (forkOf)
	ForkWhy      string `json:"fork_why,omitempty"`
	SyncMB       int64  `json:"sync_mb,omitempty"` // blocks it has taken from this node in this session while catching up
	// The lowest fee rate the peer wants to hear of, in sat/vB to one
	// decimal: only when it sent a feefilter. Core rounds what it sends on
	// purpose, so more digits would be noise.
	MinFee *float64 `json:"min_fee,omitempty"`
	// What the peer announced, for the detail that opens on a tap.
	Services []string `json:"services,omitempty"`
	Version  int      `json:"version,omitempty"` // protocol version of its version message
	PingMs   *float64 `json:"ping_ms"`
	// The lowest round trip Core has ever measured to this peer. Closer to
	// the line itself than the last ping, which also carries whatever was
	// queued on either side at that moment - so it is the one a block's last
	// hop is estimated from.
	MinPingMs   *float64        `json:"min_ping_ms,omitempty"`
	ConnTime    int64           `json:"conntime"`
	Transport   string          `json:"transport,omitempty"`
	HBTo        bool            `json:"hb_to,omitempty"`   // this peer may send us compact blocks without asking
	HBFrom      bool            `json:"hb_from,omitempty"` // we may send this peer compact blocks without asking
	BytesSent   int64           `json:"bytes_sent"`
	BytesRecv   int64           `json:"bytes_recv"`
	TrafficSent *messageTraffic `json:"traffic_sent,omitempty"`
	TrafficRecv *messageTraffic `json:"traffic_recv,omitempty"`
}

// messageTraffic groups byte counters, not counts of transactions or blocks.
// A nil map means the RPC did not supply this direction's counters.
type messageTraffic struct {
	Transactions int64 `json:"transactions"`
	Blocks       int64 `json:"blocks"`
	Addresses    int64 `json:"addresses"`
	Other        int64 `json:"other"`
}

func trafficByMessage(messages map[string]int64) *messageTraffic {
	if messages == nil {
		return nil
	}
	traffic := &messageTraffic{}
	for command, size := range messages {
		switch command {
		case "tx":
			traffic.Transactions += size
		case "block", "cmpctblock", "blocktxn", "headers":
			traffic.Blocks += size
		case "addr", "addrv2":
			traffic.Addresses += size
		default:
			traffic.Other += size
		}
	}
	return traffic
}

// group sorts a connection into one of the three buckets the map shows.
// Feeler and addr-fetch connections live for seconds and are not peers in any
// useful sense, so they are dropped ("" means skip).
func group(p rawPeer) string {
	switch p.ConnectionType {
	case "manual":
		return "manual"
	case "inbound":
		return "inbound"
	case "outbound-full-relay", "block-relay-only":
		return "outbound"
	case "feeler", "addr-fetch":
		return ""
	default:
		// Core older than 0.21 has no connection_type, and a future Core may
		// add a type: either way, show it by direction rather than hide it.
		if p.Inbound {
			return "inbound"
		}
		return "outbound"
	}
}

// hostOf strips the port from Core's addr field: "1.2.3.4:8333",
// "[2001:db8::1]:8333", "abc.onion:8333".
func hostOf(addr string) string {
	if strings.HasPrefix(addr, "[") {
		if i := strings.Index(addr, "]"); i > 0 {
			return addr[1:i]
		}
	}
	if strings.Count(addr, ":") == 1 {
		return addr[:strings.LastIndex(addr, ":")]
	}
	return addr // bare IPv6 without port, or no port at all
}

// Core reports whether a peer wants transactions announced (relaytxes) once
// the handshake is done; before that it says nothing, which is neither.
func wantsTx(relayTxes *bool) bool   { return relayTxes != nil && *relayTxes }
func refusesTx(relayTxes *bool) bool { return relayTxes != nil && !*relayTxes }

// connectedFor says the peer has been connected at least secs seconds; a
// connection time of zero or less means Core did not say.
func connectedFor(connTime int64, now time.Time, secs int64) bool {
	return connTime > 0 && now.Unix()-connTime >= secs
}

// ipOf is the IP in a getpeerinfo address, IPv4 as IPv4 even when written
// as IPv6 (::ffff:1.2.3.4); ok=false for Tor, I2P and anything else.
func ipOf(addr string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(hostOf(addr))
	return a.Unmap(), err == nil
}

// locate returns where a peer is, or ok=false when the address is not a
// public IP (Tor, I2P, CJDNS, or a private address such as the 10.21.0.1 that
// docker-proxy puts in front of inbound IPv6 on umbrelOS).
func locate(p rawPeer) (geo.Location, bool) {
	if p.mockLoc != nil {
		return *p.mockLoc, true
	}
	switch p.Network {
	case "onion", "i2p", "cjdns", "not_publicly_routable":
		return geo.Location{}, false
	}
	a, ok := ipOf(p.Addr)
	if !ok {
		return geo.Location{}, false
	}
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return geo.Location{}, false
	}
	return geo.Lookup(a)
}

// operator returns the network announcing this peer's address. Same exclusions
// as locate: an onion or I2P address belongs to no network anybody announces,
// and a Docker-relayed inbound peer carries our own address, not the peer's.
func operator(p rawPeer) (asn.Info, bool) {
	if p.mockASN != nil {
		return *p.mockASN, true
	}
	switch p.Network {
	case "onion", "i2p", "cjdns", "not_publicly_routable":
		return asn.Info{}, false
	}
	a, ok := ipOf(p.Addr)
	if !ok {
		return asn.Info{}, false
	}
	return asn.Lookup(a)
}

// dockerGateway is where inbound IPv6 comes from on umbrelOS: the Bitcoin
// container has no IPv6, so docker-proxy accepts the connection on the host
// and passes it on from the gateway of Umbrel's network, port and all. The
// peer's own address is gone. On the node this was written for: 9,935 inbound
// sessions in four weeks, against not one outbound IPv6 connection (Core
// knew 13,357 IPv6 addresses and had tried none successfully).
var dockerGateway = envOr("PEERMAP_DOCKER_GATEWAY", "10.21.0.1")

// hiddenIPv6 is the network such a peer is shown under instead of "Private".
const hiddenIPv6 = "ipv6_hidden"

// Tor and I2P peers reach Core through the proxy containers in Umbrel's
// network, which put their own address in place of the peer's. The Bitcoin
// Node app exports both addresses (exports.sh); the defaults are its values.
var (
	torProxy = envOr("APP_BITCOIN_TOR_PROXY_IP", "10.21.22.10")  // bitcoin_tor_1
	i2pProxy = envOr("APP_BITCOIN_I2P_DAEMON_IP", "10.21.22.11") // bitcoin_i2pd_daemon_1
)

const (
	torHidden = "tor_hidden"
	i2pHidden = "i2p_hidden"
)

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// isPrivate: an address inside a private network, such as the one umbrelOS
// runs its apps in.
func isPrivate(addr string) bool {
	a, ok := ipOf(addr)
	return ok && a.IsPrivate()
}

func convert(raw []rawPeer) []Peer { return convertAt(raw, time.Now()) }

// convertAt is convert on a given clock, the one the source runs on.
func convertAt(raw []rawPeer, now time.Time) []Peer {
	out := make([]Peer, 0, len(raw))
	// What the whole list says, for checks that need more than one peer:
	// the highest header any peer shares (this node's tip, as far as its
	// peers know) and whether any peer sent wtxidrelay.
	var tip int64
	anyWtxid := false
	for _, r := range raw {
		if r.SyncedHeaders != nil && *r.SyncedHeaders > tip {
			tip = *r.SyncedHeaders
		}
		if r.BytesRecvPerMsg["wtxidrelay"] > 0 {
			anyWtxid = true
		}
	}
	for _, r := range raw {
		g := group(r)
		if g == "" {
			continue
		}
		kind, relays := classify(r.Subver)
		p := Peer{
			ID: r.ID, Addr: r.Addr, Network: r.Network, Group: g, Type: r.ConnectionType,
			Subver: r.Subver, Kind: kind, About: aboutOf(r.Subver), NoRelay: !relays,
			ConnTime: r.ConnTime, Transport: r.Transport, HBTo: r.HBTo, HBFrom: r.HBFrom,
			BytesSent: r.BytesSent, BytesRecv: r.BytesRecv,
			TrafficSent: trafficByMessage(r.BytesSentPerMsg), TrafficRecv: trafficByMessage(r.BytesRecvPerMsg),
			Services: r.ServicesNames, Version: r.Version,
		}
		// An empty list is the peer saying it offers nothing; a missing field
		// is Core saying nothing. Only the first is worth showing.
		// Before the version message arrives Core lists no services, no tx
		// relay and no chain either: a peer still in its handshake has not said
		// anything yet.
		labelled := "" // the about p.Label was made from
		p.NoServices = r.Version > 0 && r.ServicesNames != nil && len(r.ServicesNames) == 0
		p.NoTxRelay = r.Version > 0 && refusesTx(r.RelayTxes)
		p.ChainUnknown = r.Version > 0 && r.SyncedHeaders != nil && *r.SyncedHeaders < 0
		// Offering nothing means no blocks to pass on, whatever the name says.
		// Bitcoin Lab strikes the same peers.
		p.NoRelay = p.NoRelay || p.NoServices
		borrowedKind, borrowedAbout, borrowedName, borrowedLabel := behindCoreName(r.Subver, r.Version, r.ServicesNames, r.RelayTxes, r.BytesRecvPerMsg)
		libreKind, libreAbout, libreLabel := libreRelay(r.Subver, r.ServicesNames)
		researcher, host, isResearch := researchHostOf(r.Addr)
		bchAbout, bchLabel := "", ""
		if bchMode {
			bchAbout, bchLabel = aboutBCH(r.Subver, kind)
		}
		switch {
		case bchAbout != "":
			// A Bitcoin Cash node, or software of another chain, by its own
			// name: none of the checks below, which hold a peer to what
			// Bitcoin Core's releases do, has anything to say about it.
			p.About, p.Label = bchAbout, bchLabel
			labelled = p.About
		case isResearch:
			p.Kind, p.About = "Research scanner", "Probably: "+researcher+"'s network research"
			// The chip names the university; the line has room for what it is.
			p.Name, p.Label = researcher, "Probably a university"
			if r.Subver != "" {
				claim := claimOf(r.Subver)
				if a := aboutOf(r.Subver); a != "" {
					claim, _, _ = strings.Cut(a, ",")
				}
				p.About = "Claims: " + claim + "\n" + p.About
			}
			labelled = p.About
			p.PoolProof = "This address is " + host + ", listed by 0xB10C"
		case p.Kind == "Wallet" && offersWhatNoWalletCan(r.ServicesNames):
			p.Kind, p.About = fakeWalletKind, fakeAbout(r.Subver, p.About, r.ServicesNames)
		case p.Kind == "Wallet" && bitcoinjTooNew(r.Subver, r.Version):
			claim := claimOf(r.Subver)
			if p.About != "" {
				claim, _, _ = strings.Cut(p.About, ",")
				claim = "the wallet " + claim
			}
			p.Kind, p.About = fakeWalletKind, "Claims: "+claim+"\nProbably: "+bitcoinjTooNewAbout+bitcoinjWhy(r, now)
		case p.Kind == fakeWalletKind:
			p.About = fakeAbout(r.Subver, "", r.ServicesNames)
		case isViaBitpeer(r.Subver, r.Version, r.ServicesNames):
			p.Kind, p.Pool, p.About, p.Label = "Pool node", "ViaBTC", viaBitpeerAbout, "Probably ViaBTC's relay"
			labelled = p.About
			p.PoolProof = "Found by 0xB10C, b10c.me"
		case libreKind != "" && protocolMismatch(r.Subver, r.Version) == "":
			p.Kind, p.About, p.Label = libreKind, libreAbout, libreLabel
			labelled = p.About
		case borrowedKind != "" &&
			behaviourAbout(r.Subver, r.Transport, r.BytesRecvPerMsg, r.ConnTime, now) == "":
			p.Kind, p.About, p.Name, p.Label = borrowedKind, borrowedAbout, borrowedName, borrowedLabel
			labelled = p.About
		default:
			// What a peer does says more than what it lacks: a crawler asking
			// for addresses again and again is named that, whatever else is off.
			// The first check that answers wins.
			for _, check := range []func() string{
				func() string { return behaviourAbout(r.Subver, r.Transport, r.BytesRecvPerMsg, r.ConnTime, now) },
				func() string { return anachronism(r.Subver, r.ServicesNames) },
				func() string { return protocolMismatch(r.Subver, r.Version) },
				func() string {
					if offersNothingAsCore(r.Subver, r.ServicesNames) {
						return offersNothingAbout
					}
					return ""
				},
				func() string {
					if skipsWtxidrelay(r.Subver, r.Version, r.BytesRecvPerMsg, anyWtxid) {
						return skippedHandshakeAbout(r.BytesRecvPerMsg)
					}
					return ""
				},
				func() string {
					return quietCoreAbout(r.Subver, r.RelayTxes, r.BytesRecvPerMsg, r.BytesSentPerMsg, r.ConnTime, now)
				},
			} {
				if why := check(); why != "" {
					p.Kind, p.About = fakeWalletKind, "Claims: "+claimOf(r.Subver)+"\nProbably: "+why
					break
				}
			}
		}
		// "Relays no blocks" came from the name. Where the name turned out
		// false - a "wallet" offering what no wallet can - it goes with it:
		// only offering nothing at all still says so.
		if kind == "Wallet" && p.Kind == fakeWalletKind {
			p.NoRelay = p.NoServices
		}
		// A pool node or crawler no rule describes (/pydroid-pool:3/,
		// /p2p-watcher:0.1.0/) gets the same line, rather than none. Where a
		// rule does describe it, the rule says what the name stands for and
		// this line what the peer did, so the second is added, not dropped.
		switch p.Kind {
		case "Other", "Pool node", "Crawler", "Light client", "Other chain":
			seen, short, noTx := sessionLine(r.ServicesNames, r.RelayTxes, r.BytesRecvPerMsg, r.BytesSentPerMsg, r.ConnTime, now)
			observed := seen != unknownAbout && seen != operatorNamedAbout
			always := p.Kind == "Other" || p.Kind == "Pool node" || p.Kind == "Crawler"
			switch {
			case p.About == "" && (observed || always):
				// The list gets the short form; the whole line is the panel's.
				p.About, p.Label = seen, short
				labelled = p.About
				p.NoTxSaid = noTx
			// "Claims:" and "Probably:" already say what the peer did.
			case p.About != "" && observed && !strings.HasPrefix(p.About, "Claims: "):
				p.Seen = observedPart(seen)
				p.NoTxSaid = noTx
			}
		}
		// Core reports version 0 until the peer's version message arrives:
		// with a version, the peer did introduce itself, only without a name.
		if p.Kind == noAgentKind && r.Version > 0 {
			p.About, p.Label = noAgentLine(r.BytesRecvPerMsg, r.ConnTime, now)
			if hemiUnnamed.MatchString(r.Subver) {
				p.About, p.Label = hemiUnnamedAbout+"\n"+p.About, "Probably Hemi's code"
			}
			labelled = p.About
		} else if p.Kind == noAgentKind {
			p.About, p.Label = "Has not said what it is: usually a connection that only checks the port, or one still starting", "Port check or starting"
			labelled = p.About
		}
		// LinkingLion with a borrowed name is Disguised; without any name it
		// claims nothing, so it stays "No user agent" and only the about says
		// who it probably is.
		if isLinkingLion(r.Addr) || r.mockLinkingLion {
			p.LinkingLion = true
			lion := linkingLionAbout
			if quietAfterHandshake(r.BytesRecvPerMsg, r.ConnTime, now) {
				lion += linkingLionQuiet
			}
			if r.Subver == "" {
				p.About, p.Label = "Probably: "+lion, "Probably LinkingLion"
				labelled = p.About
			} else {
				p.Kind, p.About = fakeWalletKind, "Claims: "+claimOf(r.Subver)+"\nProbably: "+lion
			}
		}
		// The list tells one or two reasons (tellOf); the panel gets every
		// other one, so a disguise - LinkingLion's too - stands on what the
		// peer did, not on one rule or one list of addresses alone.
		if p.Kind == fakeWalletKind {
			p.About = withAlso(p.About, disguiseEvidence(r, kind, aboutOf(r.Subver), anyWtxid, now))
			p.Tell, p.Why = tellAndWhy(p.About, p.LinkingLion)
		} else if p.Kind == "Other" || p.Kind == noAgentKind {
			p.Tell = behaviourTell(p.About)
		}
		// A proven address makes a pool's plain bitcoind a pool node too; a
		// name in the user agent only names the pool of what already is one.
		if n, byAddr := poolOf(r.Addr, r.Subver); !bchMode && n.pool != "" {
			switch {
			case byAddr && (p.Kind == "Pool node" || strings.HasPrefix(p.Kind, "Node")):
				what, short := "own Bitcoin node", "Own Bitcoin node"
				if strings.HasPrefix(aboutOf(r.Subver), "ckp2p") {
					// Whose relay, in the row itself: the pool's name otherwise
					// shows only in the filter (06.11.2026).
					what, short = "block relay (ckp2p)", n.pool+" block relay"
				}
				p.Kind, p.Pool = "Pool node", n.pool
				p.About, p.PoolProof = n.pool+"'s "+what, "This address "+n.proof
				// The pool's name has a line of its own in the list.
				p.Label, labelled = short, p.About
			case !byAddr && p.Kind == "Pool node":
				p.Pool = n.pool
			}
		}
		// A known block relay keeps its pill; the line under it says whose.
		if n, ok := relayOf(r.Addr); !bchMode && ok && (p.Kind == noAgentKind || strings.HasPrefix(p.Kind, "Node")) {
			p.About, p.Tell = n.pool+"'s block relay", n.pool+" block relay"
			p.Label, labelled = p.About, p.About
			p.PoolProof = "This address " + n.proof
		}
		if p.NoTxRelay {
			p.NoTxWhy = noTxWhy(r, p.Kind)
		}
		if !bchMode {
			p.Fork, p.ForkWhy = forkOf(r.ServicesNames, p.Kind, r.SyncedHeaders, tip)
		}
		if r.MinFeeFilter != nil && r.BytesRecvPerMsg["feefilter"] > 0 {
			v := math.Round(*r.MinFeeFilter*1e5*10) / 10
			p.MinFee = &v
		}
		p.SyncMB = syncingFrom(r.Inbound, r.BytesSentPerMsg, r.SyncedBlocks, tip, r.ConnTime, now)
		p.NoRelay = p.NoRelay || p.Fork != ""
		if r.Inbound {
			switch hostOf(r.Addr) {
			case dockerGateway:
				p.Network = hiddenIPv6
			case torProxy:
				p.Network = torHidden
			case i2pProxy:
				p.Network = i2pHidden
			}
		}
		// Umbrel's own apps reach Core from inside its network.
		if p.Kind == "Indexer" && isPrivate(r.Addr) {
			p.About = "Probably your own " + claimOf(r.Subver) + " on this Umbrel"
			p.App, _, _ = strings.Cut(strings.Trim(r.Subver, "/"), ":")
			p.Label, labelled = "Probably your own "+p.App, p.About
		}
		if net, ok := operator(r); ok {
			p.ASN, p.Operator = net.Number, net.Name
		}
		if p.Network == "ipv4" || p.Network == "ipv6" {
			p.TorExit = torexit.Is(hostOf(r.Addr))
		}
		if p.Network == "not_publicly_routable" {
			p.Operator = "Your Umbrel's own network"
		}
		if loc, ok := locate(r); ok {
			p.Country = loc.Country
			if loc.Region != "" {
				p.Region, p.RegionID = loc.Region, loc.ID
				p.Lat, p.Lon = loc.Lat, loc.Lon
			}
		}
		if r.PingTime != nil {
			ms := *r.PingTime * 1000
			p.PingMs = &ms
		}
		if r.MinPing != nil {
			ms := *r.MinPing * 1000
			p.MinPingMs = &ms
		}
		// A rule's label and chip go with its sentence only: where a later
		// step put another about in its place, they go too.
		// A label stands for the about it was made from: a later step that
		// replaced the about (LinkingLion, a pool's address) takes it away.
		if p.About != labelled {
			p.Label = ""
		}
		if rule, ok := ruleOf(r.Subver); ok && p.About == rule.text {
			p.Label = rule.label
			if p.Name == "" {
				p.Name = rule.name
			}
		}
		// A disguise or a LinkingLion range gives its reasons from what the
		// peer did already; the session line would say it twice.
		if p.Seen != "" && (strings.HasPrefix(p.About, "Claims: ") || strings.HasPrefix(p.About, "Probably: ")) {
			p.Seen, p.NoTxSaid = "", false
		}
		out = append(out, p)
	}
	return out
}

// noTxWhy says why a peer wants no transactions, where that can be told.
// Core asks a peer for addresses once on the connections it opens itself,
// except on its two block-relay-only ones, which also send no transactions
// (net_processing.cpp: SetupAddressRelay returns false for them, so no
// getaddr). So a real Core or Knots that connected to this node with
// transactions off and asked for addresses runs with blocksonly=1; one that
// did not ask opened the connection for blocks only. Only once its handshake
// is done (verack) and only for a node, not for software borrowing the name.
func noTxWhy(r rawPeer, kind string) string {
	if r.ConnectionType == "block-relay-only" {
		return "Your node opened this connection for blocks only"
	}
	recv := r.BytesRecvPerMsg
	if r.Inbound && (strings.HasPrefix(kind, "Node (") || kind == "Pool node") && claimedCore.MatchString(r.Subver) &&
		recv["verack"] > 0 && recv["wtxidrelay"] > 0 {
		if recv["getaddr"] > 0 {
			return "Its node probably runs with blocksonly=1: it wants no transactions from other nodes"
		}
		return "Its node opened this connection for blocks only"
	}
	return "Wants no transactions from your node"
}
