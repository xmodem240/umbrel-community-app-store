package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/benunskilled/peer-map/geo"
)

func TestGroup(t *testing.T) {
	cases := []struct {
		typ     string
		inbound bool
		want    string
	}{
		{"manual", false, "manual"},
		{"inbound", true, "inbound"},
		{"outbound-full-relay", false, "outbound"},
		{"block-relay-only", false, "outbound"},
		{"feeler", false, ""},
		{"addr-fetch", false, ""},
		{"", true, "inbound"},
		{"", false, "outbound"},
		{"something-new", true, "inbound"},
	}
	for _, c := range cases {
		if got := group(rawPeer{ConnectionType: c.typ, Inbound: c.inbound}); got != c.want {
			t.Errorf("%q inbound=%v: got %q want %q", c.typ, c.inbound, got, c.want)
		}
	}
}

func TestHostOf(t *testing.T) {
	for in, want := range map[string]string{
		"1.0.0.1:8333":           "1.0.0.1",
		"[2001::1]:8333":         "2001::1",
		"abc.onion:8333":         "abc.onion",
		"2001::1":                "2001::1",
		"10.21.0.1:51234":        "10.21.0.1",
		"[::ffff:1.0.0.1]:40000": "::ffff:1.0.0.1",
	} {
		if got := hostOf(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestLocate(t *testing.T) {
	for _, c := range []struct{ addr, network, cc, region string }{
		{"1.0.0.1:8333", "ipv4", "AU", "Queensland"},
		{"[2a01:4f8::1]:8333", "ipv6", "DE", "Bavaria"},
		{"[::ffff:1.0.0.1]:8333", "ipv4", "AU", "Queensland"},
		{"10.21.0.1:51234", "not_publicly_routable", "", ""},
		{"10.21.0.1:51234", "ipv4", "", ""}, // private even if Core labelled it ipv4
		{"127.0.0.1:50122", "onion", "", ""},
		{"x.b32.i2p:0", "i2p", "", ""},
		{"[fc00::1]:8333", "cjdns", "", ""},
	} {
		loc, ok := locate(rawPeer{Addr: c.addr, Network: c.network})
		if ok != (c.cc != "") || loc.Country != c.cc || loc.Region != c.region {
			t.Errorf("%s/%s: got %q/%q ok=%v want %q/%q", c.addr, c.network, loc.Country, loc.Region, ok, c.cc, c.region)
		}
	}
}

// Same exclusions as locate, and one case the two answer differently: geo puts
// 1.0.2.200 in China, no network announces it. Where a peer is and whose
// machine it is really are two lookups.
func TestOperator(t *testing.T) {
	for _, c := range []struct {
		addr, network string
		asn           uint32
	}{
		{"1.0.0.1:8333", "ipv4", 13335},       // Cloudflare
		{"[2a01:4f8::1]:8333", "ipv6", 24940}, // Hetzner
		{"[::ffff:1.0.0.1]:8333", "ipv4", 13335},
		{"1.0.2.200:8333", "ipv4", 0}, // located, but announced by nobody
		{"10.21.0.1:51234", "not_publicly_routable", 0},
		{"10.21.0.1:51234", "ipv4", 0},
		{"127.0.0.1:50122", "onion", 0},
		{"x.b32.i2p:0", "i2p", 0},
		{"[fc00::1]:8333", "cjdns", 0},
	} {
		net, ok := operator(rawPeer{Addr: c.addr, Network: c.network})
		if ok != (c.asn != 0) || net.Number != c.asn {
			t.Errorf("%s/%s: got AS%d (%q, ok=%v) want AS%d", c.addr, c.network, net.Number, net.Name, ok, c.asn)
		}
		if ok && net.Name == "" {
			t.Errorf("%s: operator name missing", c.addr)
		}
	}
}

// A getpeerinfo reply shaped like Bitcoin Core's (fields trimmed to what a
// real node sends for these connection types).
const coreReply = `{"result":[
 {"id":0,"addr":"1.0.0.1:8333","network":"ipv4","connection_type":"manual","inbound":false,"subver":"/Satoshi:31.1.0/","pingtime":0.0421,"conntime":1757000000,"transport_protocol_type":"v2","bytessent":10,"bytesrecv":20},
 {"id":1,"addr":"[2a01:4f8::1]:48222","network":"ipv6","connection_type":"inbound","inbound":true,"subver":"/Satoshi:30.0.0/","conntime":1757000001,"transport_protocol_type":"v1","bytessent":1,"bytesrecv":2},
 {"id":2,"addr":"1.0.2.200:8333","network":"ipv4","connection_type":"block-relay-only","inbound":false,"subver":"","pingtime":0.1,"conntime":1757000002},
 {"id":3,"addr":"1.0.2.201:8333","network":"ipv4","connection_type":"feeler","inbound":false}
],"error":null,"id":"peermap"}`

func TestRPCAndConvert(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		gotAuth = u + ":" + p
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		if u != "umbrel" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		io := strings.NewReader(coreReply)
		w.Header().Set("Content-Type", "application/json")
		io.WriteTo(w)
	}))
	defer srv.Close()

	c := &rpcClient{url: srv.URL + "/", user: "umbrel", pass: "secret", http: srv.Client()}
	raw, err := c.getPeerInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "umbrel:secret" || !strings.Contains(gotBody, `"method":"getpeerinfo"`) {
		t.Fatalf("auth %q body %q", gotAuth, gotBody)
	}
	peers := convert(raw)
	if len(peers) != 3 {
		t.Fatalf("want 3 peers (feeler dropped), got %d", len(peers))
	}
	want := []struct{ group, cc, region string }{{"manual", "AU", "Queensland"}, {"inbound", "DE", "Bavaria"}, {"outbound", "CN", "Fujian"}}
	for i, w := range want {
		p := peers[i]
		if p.Group != w.group || p.Country != w.cc || p.Region != w.region || p.RegionID == 0 || p.Lat == 0 {
			t.Errorf("peer %d: got %s/%s/%s rid=%d lat=%v want %s/%s/%s", i, p.Group, p.Country, p.Region, p.RegionID, p.Lat, w.group, w.cc, w.region)
		}
	}
	if peers[0].PingMs == nil || *peers[0].PingMs < 42 || *peers[0].PingMs > 42.2 {
		t.Errorf("ping not converted to ms: %v", peers[0].PingMs)
	}
	if peers[1].PingMs != nil {
		t.Errorf("missing pingtime must stay null")
	}

	bad := &rpcClient{url: srv.URL + "/", user: "wrong", pass: "x", http: srv.Client()}
	if _, err := bad.getPeerInfo(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("want 401 error, got %v", err)
	}
}

func TestHighBandwidthDirections(t *testing.T) {
	for _, c := range []struct {
		name, fields string
		to, from     bool
	}{
		{"missing on older Core", "", false, false},
		{"neither", `,"bip152_hb_to":false,"bip152_hb_from":false`, false, false},
		{"peer sends to us", `,"bip152_hb_to":true,"bip152_hb_from":false`, true, false},
		{"we send to peer", `,"bip152_hb_to":false,"bip152_hb_from":true`, false, true},
		{"both", `,"bip152_hb_to":true,"bip152_hb_from":true`, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var raw []rawPeer
			if err := json.Unmarshal([]byte(`[{"id":1,"addr":"192.0.2.1:8333","connection_type":"manual","subver":"/Satoshi:31.1.0/"`+c.fields+`}]`), &raw); err != nil {
				t.Fatal(err)
			}
			peers := convert(raw)
			if len(peers) != 1 || peers[0].HBTo != c.to || peers[0].HBFrom != c.from {
				t.Fatalf("directions changed during conversion: %+v", peers)
			}
			encoded, err := json.Marshal(peers[0])
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatal(err)
			}
			for key, active := range map[string]bool{"hb_to": c.to, "hb_from": c.from} {
				value, present := payload[key]
				if present != active || active && value != true {
					t.Errorf("%s: got %v (present %v), want active %v", key, value, present, active)
				}
			}
		})
	}
}

func TestRPCErrorObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"result":null,"error":{"code":-28,"message":"Loading block index..."},"id":"peermap"}`))
	}))
	defer srv.Close()
	c := &rpcClient{url: srv.URL + "/", http: srv.Client()}
	if _, err := c.getPeerInfo(context.Background()); err == nil || !strings.Contains(err.Error(), "Loading block index") {
		t.Errorf("got %v", err)
	}
}

func TestAtMostOneCallPerInterval(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	calls := 0
	fail := false
	s := &source{
		now: func() time.Time { return now },
		fetch: func(context.Context) ([]rawPeer, error) {
			calls++
			if fail {
				return nil, errors.New("node down")
			}
			return []rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "manual"}}, nil
		},
	}
	ctx := context.Background()
	for i := 0; i < 50; i++ { // many browsers inside one window
		s.get(ctx)
	}
	if calls != 1 {
		t.Fatalf("50 requests in one window made %d RPC calls", calls)
	}
	now = now.Add(interval - time.Second)
	if snap := s.get(ctx); calls != 1 || snap.NextIn != 1 {
		t.Fatalf("calls=%d next_in=%d", calls, snap.NextIn)
	}
	now = now.Add(time.Second)
	fail = true
	snap := s.get(ctx)
	// What the failure says is TestRPCFailureStaysOutOfTheSnapshot's business;
	// here it only has to say something and leave the peers alone.
	if calls != 2 || snap.Error == "" || len(snap.Peers) != 1 {
		t.Fatalf("after failure: calls=%d err=%q peers=%d (last good list must stay)", calls, snap.Error, len(snap.Peers))
	}
	s.get(ctx) // failure does not shorten the window
	if calls != 2 {
		t.Fatalf("retried inside window after failure: calls=%d", calls)
	}
}

// A browser that goes away mid-poll must not take the RPC with it: the call
// belongs to the app, and its answer is what every other open dashboard reads
// for the rest of the window.
func TestTheRPCOutlivesTheBrowserRequest(t *testing.T) {
	var gotErr error
	var hadDeadline bool
	s := &source{
		now: time.Now,
		fetch: func(ctx context.Context) ([]rawPeer, error) {
			gotErr = ctx.Err()
			_, hadDeadline = ctx.Deadline()
			return []rawPeer{{Addr: "1.0.0.1:8333", ConnectionType: "manual"}}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the dashboard is gone before Core is even asked

	snap := s.get(ctx)
	if gotErr != nil {
		t.Fatalf("the RPC was handed the dead browser context: %v", gotErr)
	}
	if !hadDeadline {
		t.Error("detaching the request must not lose the 20s timeout")
	}
	if snap.Error != "" || len(snap.Peers) != 1 {
		t.Fatalf("err=%q peers=%d - the answer must still reach the snapshot", snap.Error, len(snap.Peers))
	}
}

// Go's *url.Error prints the address it failed to reach, which here is the
// node's host and port. The browser gets a line it can act on; the address
// stays in the log.
func TestRPCFailureStaysOutOfTheSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/"
	srv.Close() // nothing listens there any more
	addr := strings.TrimPrefix(srv.URL, "http://")

	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	c := &rpcClient{url: url, http: &http.Client{}}
	s := &source{now: time.Now, fetch: c.getPeerInfo}
	snap := s.get(context.Background())

	if snap.Error == "" {
		t.Fatal("a node that is not there must still say so on the dashboard")
	}
	if strings.Contains(snap.Error, addr) || strings.Contains(snap.Error, "http") {
		t.Errorf("the snapshot names the node: %q", snap.Error)
	}
	if !strings.Contains(logged.String(), addr) {
		t.Errorf("the detail never reached the log: %q", logged.String())
	}
}

// Only lines this app wrote reach the browser; anything else is answered
// vaguely rather than passed through.
func TestShownText(t *testing.T) {
	refused := errors.New("dial tcp 10.21.0.1:8332: connect: connection refused")
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"a line with nothing behind it", &shownError{line: "rejected the RPC credentials (401)"}, "rejected the RPC credentials (401)"},
		{"a line in front of a detail", &shownError{line: "unreachable from this app", err: refused}, "unreachable from this app"},
		{"wrapped further up", fmt.Errorf("getpeerinfo: %w", &shownError{line: "unreachable from this app", err: refused}), "unreachable from this app"},
		{"an error that never passed through here", refused, "could not be asked for its peers"},
	} {
		if got := shownText(c.err); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// default-src is not a fallback for base-uri or form-action: without them an
// injected <base> could re-point every relative URL on the page, and an
// injected form could post off-site. The page has neither, so both are 'none'.
func TestCSPClosesBaseAndForm(t *testing.T) {
	h := newHandler(&source{now: time.Now, fetch: mockPeers}, offSibling())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("the page no longer loads: HTTP %d", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q is missing %q", csp, want)
		}
	}
	page, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"<base", "<form"} {
		if strings.Contains(string(page), tag) {
			t.Errorf("the page now has a %s, which 'none' forbids", tag)
		}
	}
}

func TestHTTP(t *testing.T) {
	s := &source{now: time.Now, fetch: mockPeers}
	h := newHandler(s, offSibling())
	for _, path := range []string{"/", "/world.json", "/app.js", "/style.css", "/api/health"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Errorf("%s: HTTP %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/peers", nil))
	var snap snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	groups := map[string]int{}
	placed := 0
	for _, p := range snap.Peers {
		groups[p.Group]++
		if p.Type == "feeler" {
			t.Error("feeler leaked into output")
		}
		if p.Country != "" {
			placed++
		}
	}
	if groups["manual"] != 8 || groups["outbound"] != 12 || groups["inbound"] != 28 { // 22 public + 2 demo LinkingLion + 2 Docker IPv6 + Tor + I2P
		t.Errorf("groups %v", groups)
	}
	if placed != 8+11+22+2 { // + the two demo LinkingLion
		t.Errorf("placed %d peers on the map, want 43", placed)
	}
	for _, p := range snap.Peers {
		h := hostOf(p.Addr)
		if p.Network == "ipv4" && !(strings.HasPrefix(h, "192.0.2.") || strings.HasPrefix(h, "198.51.100.") || strings.HasPrefix(h, "203.0.113.")) {
			t.Errorf("demo IPv4 %s is not a documentation address", h)
		}
		if p.Network == "ipv6" && !strings.HasPrefix(h, "2001:db8:") {
			t.Errorf("demo IPv6 %s is not a documentation address", h)
		}
	}
}

func TestHomeOfVotesByRegionAmongOwnConnections(t *testing.T) {
	want, ok := geoLookup("8.8.8.8")
	if !ok {
		t.Skip("bundled database does not place 8.8.8.8")
	}
	raw := []rawPeer{
		{AddrLocal: "10.21.21.8:8333"},             // private: ignored
		{AddrLocal: ""},                            // nothing reported
		{AddrLocal: "100.64.0.9:8333"},             // CGNAT: cannot be placed, ignored
		{AddrLocal: "[::ffff:8.8.8.8]:8333"},       // IPv4-mapped counts as 8.8.8.8
		{AddrLocal: "8.8.4.4:8333"},                // same region, different address
		{AddrLocal: "1.1.1.1:8333", Inbound: true}, // inbound peers do not vote ...
		{AddrLocal: "1.1.1.1:8333", Inbound: true}, // ... however many there are
		{AddrLocal: "1.1.1.1:8333", Inbound: true},
	}
	h := homeOf(raw)
	if h == nil || h.Country != want.Country || h.Region != want.Region {
		t.Fatalf("home = %+v, want %s/%s", h, want.Country, want.Region)
	}
}

func TestHomeOfFallsBackToInboundAndOtherwiseNil(t *testing.T) {
	if h := homeOf([]rawPeer{{AddrLocal: "10.21.21.8:8333"}, {AddrLocal: "100.64.0.9:8333"}}); h != nil {
		t.Fatalf("home = %+v, want nil without a placeable address", h)
	}
	want, ok := geoLookup("8.8.8.8")
	if !ok {
		t.Skip("bundled database does not place 8.8.8.8")
	}
	h := homeOf([]rawPeer{{AddrLocal: "8.8.8.8:8333", Inbound: true}})
	if h == nil || h.Country != want.Country {
		t.Fatalf("home = %+v, want the inbound peers' answer when no outbound peer reports one", h)
	}
}

func geoLookup(ip string) (geo.Location, bool) {
	return geo.Lookup(netip.MustParseAddr(ip))
}

func TestTorExitPeer(t *testing.T) {
	list, err := os.ReadFile("torexit/exits.txt")
	if err != nil {
		t.Fatal(err)
	}
	exit, _, _ := strings.Cut(string(list), "\n")
	got := convert([]rawPeer{
		{Addr: exit + ":51234", Network: "ipv4", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:30.2.0/"},
		{Addr: "192.0.2.1:51234", Network: "ipv4", ConnectionType: "inbound", Inbound: true, Subver: "/Satoshi:30.2.0/"},
	})
	if !got[0].TorExit || got[1].TorExit {
		t.Errorf("tor_exit = %v, %v; want true, false", got[0].TorExit, got[1].TorExit)
	}
}

func TestRPCWithoutResultIsNotEmpty(t *testing.T) {
	// A reply with no list used to read as "no peers" and replace the last good map.
	for _, body := range []string{`{"result":null,"error":null}`, `{}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		}))
		c := &rpcClient{url: srv.URL + "/", http: srv.Client()}
		if _, err := c.getPeerInfo(context.Background()); err == nil || !strings.Contains(err.Error(), "without a result") {
			t.Errorf("%s: got %v", body, err)
		}
		srv.Close()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":[],"error":null}`))
	}))
	defer srv.Close()
	c := &rpcClient{url: srv.URL + "/", http: srv.Client()}
	if peers, err := c.getPeerInfo(context.Background()); err != nil || len(peers) != 0 {
		t.Errorf("an empty list is a real answer: got %v, %v", peers, err)
	}
}

func TestNoTxWhy(t *testing.T) {
	no := false
	all := []string{"NETWORK", "WITNESS", "NETWORK_LIMITED", "P2P_V2"}
	base := rawPeer{Addr: "1.0.0.1:8333", ConnectionType: "inbound", Inbound: true, Version: 70016,
		Subver: "/Satoshi:31.1.0/", Transport: "v2", ServicesNames: all, RelayTxes: &no,
		BytesRecvPerMsg: map[string]int64{"version": 126, "wtxidrelay": 13, "sendaddrv2": 13, "verack": 13}}
	cases := []struct {
		name string
		edit func(r *rawPeer)
		want string
	}{
		{"inbound block-relay-only", func(r *rawPeer) {}, "Its node opened this connection for blocks only"},
		{"inbound blocksonly=1", func(r *rawPeer) { r.BytesRecvPerMsg = map[string]int64{"wtxidrelay": 13, "verack": 13, "getaddr": 33} },
			"Its node probably runs with blocksonly=1: it wants no transactions from other nodes"},
		{"our own block-relay-only", func(r *rawPeer) { r.Inbound, r.ConnectionType = false, "block-relay-only" },
			"Your node opened this connection for blocks only"},
		{"handshake not done", func(r *rawPeer) { r.BytesRecvPerMsg = map[string]int64{"version": 126} }, "Wants no transactions from your node"},
		{"not Core", func(r *rawPeer) { r.Subver = "/btcwire:0.5.0/neutrino:0.12.0-beta/" }, "Wants no transactions from your node"},
	}
	for _, c := range cases {
		r := base
		c.edit(&r)
		if got := convert([]rawPeer{r})[0].NoTxWhy; got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	yes := true
	r := base
	r.RelayTxes = &yes
	if got := convert([]rawPeer{r})[0].NoTxWhy; got != "" {
		t.Errorf("relaying peer: got %q", got)
	}
}

// The map's viewBox stands in index.html so the page has its height before
// world.json is loaded; it has to be world.json's own size.
func TestMapSizeInHTML(t *testing.T) {
	var world struct{ W, H float64 }
	b, err := os.ReadFile("web/world.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &world); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`<svg id="map" viewBox="0 0 %g %g"`, world.W, world.H)
	if !strings.Contains(string(html), want) {
		t.Errorf("index.html has no %s", want)
	}
}

// The fee filter in sat/vB, to one decimal, and only from a peer that sent one.
func TestMinFee(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		btcPerKvB *float64
		sent      int64
		want      string
	}{
		{f(0.000001), 32, "0.1"},
		{f(0.00001), 32, "1"},
		{f(0.0000123), 32, "1.2"},
		{f(0), 32, "0"},
		{f(0.00001), 0, "none"},
		{nil, 32, "none"},
	} {
		r := rawPeer{ID: 1, Addr: "203.0.113.9:8333", ConnectionType: "outbound-full-relay", Subver: "/Satoshi:31.1.0/", Version: 70016,
			MinFeeFilter: c.btcPerKvB, BytesRecvPerMsg: map[string]int64{"feefilter": c.sent}}
		p := convert([]rawPeer{r})[0]
		got := "none"
		if p.MinFee != nil {
			got = strconv.FormatFloat(*p.MinFee, 'f', -1, 64)
		}
		if got != c.want {
			t.Errorf("%v sent %d: %s, want %s", c.btcPerKvB, c.sent, got, c.want)
		}
	}
}

func TestTrafficByMessage(t *testing.T) {
	if trafficByMessage(nil) != nil {
		t.Fatal("missing counters must stay unavailable")
	}
	got := trafficByMessage(map[string]int64{"tx": 100, "block": 200, "cmpctblock": 30, "blocktxn": 40, "headers": 50, "addr": 60, "addrv2": 70, "getdata": 80, "inv": 90, "future-command": 10})
	want := messageTraffic{Transactions: 100, Blocks: 320, Addresses: 130, Other: 180}
	if *got != want {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
	if got := trafficByMessage(map[string]int64{}); got == nil || *got != (messageTraffic{}) {
		t.Fatal("empty counters must remain known zeroes")
	}
}

func TestTrafficDirections(t *testing.T) {
	raw := []rawPeer{{ConnectionType: "inbound", BytesSentPerMsg: map[string]int64{"tx": 123}, BytesRecvPerMsg: map[string]int64{"block": 456}}}
	peers := convertAt(raw, time.Now())
	if len(peers) != 1 {
		t.Fatalf("got %d peers", len(peers))
	}
	if peers[0].TrafficSent.Transactions != 123 || peers[0].TrafficRecv.Blocks != 456 {
		t.Fatal("traffic directions were swapped or dropped")
	}
}

func TestListenerWithoutMultipathTCP(t *testing.T) {
	if listenConfig().MultipathTCP() {
		t.Fatal("the dashboard listener must not accept Multipath TCP")
	}
}
