package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSiblingInstalled(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := &siblingCheck{url: srv.URL, client: srv.Client(), every: time.Minute}
	if !s.installed(context.Background()) {
		t.Fatal("a neighbour that answers 200 must count as installed")
	}
	// Second call inside the window must not ask again: a dashboard polls every
	// ten seconds and an app is not installed twice a minute.
	s.installed(context.Background())
	if calls != 1 {
		t.Errorf("asked %d times, want 1 - the answer is cached", calls)
	}
}

func TestSiblingMissingIsSilent(t *testing.T) {
	// A port nobody listens on: the normal case for someone who installed one
	// app and not the other.
	s := &siblingCheck{url: "http://127.0.0.1:1/api/health", client: &http.Client{Timeout: time.Second}, every: time.Minute}
	if s.installed(context.Background()) {
		t.Error("an unreachable neighbour must not produce a link")
	}
}

func TestSiblingOff(t *testing.T) {
	if offSibling().installed(context.Background()) {
		t.Error(`url "off" must switch the check off entirely`)
	}
}

// A neighbour reached through a transport that answers the health probe and
// cuts off the block request, or refuses both - whatever err says.
type stubNeighbour struct {
	healthOK bool
	err      error
	calls    map[string]int
}

func (t *stubNeighbour) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls[r.URL.Path]++
	if t.healthOK && r.URL.Path == "/api/health" {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	}
	return nil, &url.Error{Op: r.Method, URL: r.URL.String(), Err: t.err}
}

func neighbour(tr *stubNeighbour) *siblingCheck {
	return &siblingCheck{
		url:      "http://bitcoinlab-node_dashboard_1:8788/api/health",
		blockURL: "http://bitcoinlab-node_dashboard_1:8788/api/blocks/latest",
		client:   &http.Client{Transport: tr},
		every:    5 * time.Minute,
		now:      time.Now,
	}
}

// The probe belongs to this app, not to the browser request that triggered it:
// a dashboard that goes away mid-poll must not be able to hide the link from
// everyone else.
func TestSiblingProbeOutlivesTheBrowserRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := &siblingCheck{url: srv.URL, client: srv.Client(), every: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !s.installed(ctx) {
		t.Error("a cancelled browser request cancelled the probe with it")
	}
}

// A neighbour that refuses the connection has answered - that is the normal
// case, remembered for five minutes so a missing app costs no request per
// poll. A cancellation cannot reach the probe: it runs detached from the
// browser's request (TestSiblingProbeOutlivesTheBrowserRequest).
func TestSiblingRemembersARefusal(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"a refused connection is an answer", errors.New("connect: connection refused"), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			tr := &stubNeighbour{err: c.err, calls: map[string]int{}}
			s := neighbour(tr)
			if s.installed(context.Background()) || s.installed(context.Background()) {
				t.Fatal("a neighbour that never answered must not produce a link")
			}
			if got := tr.calls["/api/health"]; got != c.want {
				t.Errorf("probed %d times, want %d", got, c.want)
			}
		})
	}
}

// The same rule one level down, where the cost of getting it wrong is an
// empty block card for the rest of the poll interval.
func TestSiblingBlockRemembersARefusal(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"a refused connection is an answer", errors.New("connect: connection refused"), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			tr := &stubNeighbour{healthOK: true, err: c.err, calls: map[string]int{}}
			s := neighbour(tr)
			if s.latest(context.Background()) != nil || s.latest(context.Background()) != nil {
				t.Fatal("a neighbour that sent no block must not produce one")
			}
			if got := tr.calls["/api/blocks/latest"]; got != c.want {
				t.Errorf("asked %d times, want %d", got, c.want)
			}
		})
	}
}

func TestSiblingLatestBlock(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	detected := now.Add(-30 * time.Second).UnixMilli()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/api/blocks/latest" {
			t.Errorf("asked for %q", r.URL.Path)
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"hash":"00beef","height":967724,"detectedAt":%d,"pool":"Foundry",
			"poolName":"Foundry USA","poolTag":"Foundry USA Pool","poolSource":"address",
			"firstPeers":["1.2.3.4:8333"],"eligible":204,
			"stratum":{"createdAt":%d,"entries":[
				{"label":"Public A","own":false,"latencyMs":0,"rank":1,"miss":false},
				{"label":"My pool","own":true,"latencyMs":412.5,"rank":2,"miss":false},
				{"label":"Quiet one","own":false,"latencyMs":null,"rank":null,"miss":true}]},
			"templateMs":84.5,"firstPingMs":98,"firstHb":0,"deliveredEver":["1.2.3.4:8333","5.6.7.8:8333"],
			"recon":"asked","reconMissingBytes":38000,"blockSize":1520000,
			"reconLast100":{"blocks":98,"mempool":60,"asked":35,"full":3,"askedAvgBytes":38200},
			"routeMedian":{"blocks":100,"core":{"ms":210,"n":100},"peer":{"ms":120,"n":97},
				"template":{"ms":295,"n":60},"own":{"ms":420,"n":99},"ownLabel":"My pool"}}`, detected, detected-225)
	}))
	defer srv.Close()

	s := &siblingCheck{
		url:      srv.URL + "/api/health",
		blockURL: srv.URL + "/api/blocks/latest",
		client:   srv.Client(),
		every:    time.Minute,
		now:      func() time.Time { return now },
	}

	got := s.latest(context.Background())
	if got == nil {
		t.Fatal("no block")
	}
	if got.Height != 967724 || got.Pool != "Foundry" || got.PoolName != "Foundry USA" {
		t.Errorf("got %+v", got)
	}
	if len(got.FirstPeers) != 1 || got.FirstPeers[0] != "1.2.3.4:8333" {
		t.Errorf("first peers: %v", got.FirstPeers)
	}
	// The age is what the browser marks by, so it has to be the block's real
	// age and not the age of this reply.
	if got.AgeMs != 30_000 {
		t.Errorf("age %d ms, want 30000", got.AgeMs)
	}
	if got.MarkForMs != blockMarkFor.Milliseconds() {
		t.Errorf("mark window %d ms", got.MarkForMs)
	}
	// The three roles arrive separately and stay separate: mined, delivered,
	// and turned into work.
	// How the block was rebuilt rides along; an empty window is left out.
	if got.FirstHb == nil || *got.FirstHb != 0 {
		t.Errorf("first_hb %v, want 0 (known, and not high-bandwidth)", got.FirstHb)
	}
	if got.Recon != "asked" || got.ReconMissingBytes == nil || *got.ReconMissingBytes != 38000 {
		t.Errorf("recon %q %v", got.Recon, got.ReconMissingBytes)
	}
	if got.BlockSize == nil || *got.BlockSize != 1520000 {
		t.Errorf("block size %v", got.BlockSize)
	}
	if got.ReconLast100 == nil || got.ReconLast100.Blocks != 98 || got.ReconLast100.AskedAvgBytes == nil || *got.ReconLast100.AskedAvgBytes != 38200 {
		t.Errorf("recon last 100 %+v", got.ReconLast100)
	}
	if got.Eligible != 204 {
		t.Errorf("eligible %d, want 204", got.Eligible)
	}
	if got.Stratum == nil || len(got.Stratum.Entries) != 3 {
		t.Fatalf("stratum race not carried through: %+v", got.Stratum)
	}
	if !got.Stratum.Entries[1].Own {
		t.Error("the owner's own pool must stay marked as his")
	}
	if got.Stratum.Entries[2].LatencyMs != nil || !got.Stratum.Entries[2].Miss {
		t.Error("a pool that reported nothing must stay in the list as a miss")
	}
	// The first job came 225 ms before Core announced the block, so on the
	// route Core stands 225 ms after the zero.
	if got.Stratum.CoreMs == nil || *got.Stratum.CoreMs != 225 {
		t.Errorf("core_ms = %v, want 225", got.Stratum.CoreMs)
	}
	// The route's inner stops and the typical route arrive intact.
	if got.TemplateMs == nil || *got.TemplateMs != 84.5 || got.FirstPingMs == nil || *got.FirstPingMs != 98 {
		t.Errorf("template %v, ping %v", got.TemplateMs, got.FirstPingMs)
	}
	if len(got.DeliveredEver) != 2 || got.DeliveredEver[1] != "5.6.7.8:8333" {
		t.Errorf("delivered ever: %v", got.DeliveredEver)
	}
	m := got.RouteMedian
	if m == nil || m.Blocks != 100 || m.Template == nil || m.Template.Ms != 295 || m.Template.N != 60 || m.OwnLabel != "My pool" {
		t.Errorf("route median %+v", m)
	}

	// Inside the poll interval the answer is cached - but it must age while it
	// sits there, or a cached copy would claim the block is younger than it is.
	now = now.Add(5 * time.Second)
	again := s.latest(context.Background())
	if calls != 1 {
		t.Errorf("asked the neighbour %d times, want 1", calls)
	}
	if again.AgeMs != 35_000 {
		t.Errorf("cached age %d ms, want 35000", again.AgeMs)
	}
}

func TestSiblingLatestWithoutNeighbour(t *testing.T) {
	if offSibling().latest(context.Background()) != nil {
		t.Error(`"off" must not produce a block`)
	}
	s := &siblingCheck{
		url:      "http://127.0.0.1:1/api/health",
		blockURL: "http://127.0.0.1:1/api/blocks/latest",
		client:   &http.Client{Timeout: time.Second},
		every:    time.Minute,
		now:      time.Now,
	}
	if s.latest(context.Background()) != nil {
		t.Error("an unreachable neighbour must not produce a block")
	}
}

// Where Core stands on the route, counted from the first job. Negative is a
// real answer - Core can have the block before any pool sends a job - and a
// race with no start instant has no zero to count from at all.
func TestSiblingCoreOffset(t *testing.T) {
	for _, tc := range []struct {
		name      string
		createdAt string
		want      *int64
	}{
		{"core after the first job", "1789900000000", ptr(int64(300))},
		{"core before any job", "1789900000500", ptr(int64(-200))},
		{"no start instant", "0", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/health" {
					w.WriteHeader(http.StatusOK)
					return
				}
				fmt.Fprintf(w, `{"hash":"00beef","height":1,"detectedAt":1789900000300,
					"stratum":{"createdAt":%s,"entries":[{"label":"A","latencyMs":0,"rank":1}]}}`, tc.createdAt)
			}))
			defer srv.Close()
			s := &siblingCheck{
				url: srv.URL + "/api/health", blockURL: srv.URL + "/api/blocks/latest",
				client: srv.Client(), every: time.Minute,
				now: func() time.Time { return time.UnixMilli(1789900010000) },
			}
			got := s.latest(context.Background())
			if got == nil || got.Stratum == nil {
				t.Fatal("no block or no race")
			}
			switch {
			case tc.want == nil && got.Stratum.CoreMs != nil:
				t.Errorf("core_ms = %d, want none", *got.Stratum.CoreMs)
			case tc.want != nil && (got.Stratum.CoreMs == nil || *got.Stratum.CoreMs != *tc.want):
				t.Errorf("core_ms = %v, want %d", got.Stratum.CoreMs, *tc.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// A reply past the 64 KB this reads used to cost the block card without a
// word anywhere. It still costs the card - a truncated reply is not one to
// show - but the log now says why, once, instead of nothing every ten seconds.
func TestSiblingSaysSoWhenTheReplyIsTooLarge(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			return
		}
		many := make([]string, 4000)
		for i := range many {
			many[i] = fmt.Sprintf(`"[2001:db8::%x]:51234"`, i)
		}
		fmt.Fprintf(w, `{"hash":"00beef","height":1,"detectedAt":%d,"deliveredEver":[%s]}`,
			now.UnixMilli(), strings.Join(many, ","))
	}))
	defer srv.Close()

	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	clock := now
	s := &siblingCheck{
		url:      srv.URL + "/api/health",
		blockURL: srv.URL + "/api/blocks/latest",
		client:   srv.Client(),
		every:    time.Hour,
		now:      func() time.Time { return clock },
	}
	if got := s.latest(context.Background()); got != nil {
		t.Fatalf("a truncated reply was shown: %+v", got)
	}
	if !strings.Contains(logged.String(), "64 KB") {
		t.Fatalf("nothing logged about the size; log: %q", logged.String())
	}
	clock = clock.Add(interval)
	s.latest(context.Background())
	if n := strings.Count(logged.String(), "64 KB"); n != 1 {
		t.Errorf("logged %d times, want once while it stays too large", n)
	}
}

// The browser link to Bitcoin Lab uses PEERMAP_SIBLING_LINK_PORT only when it is
// a valid port; anything else leaves the store port to the page.
func TestSiblingLinkPort(t *testing.T) {
	for v, want := range map[string]int{"": 0, "8794": 8794, "0": 0, "70000": 0, "abc": 0} {
		t.Setenv("PEERMAP_SIBLING_LINK_PORT", v)
		if got := siblingLinkPort(); got != want {
			t.Errorf("PEERMAP_SIBLING_LINK_PORT=%q: got %d, want %d", v, got, want)
		}
	}
}

// offSibling is the check a test wants: no neighbour, no request, no timeout.
func offSibling() *siblingCheck {
	return &siblingCheck{url: "off", now: time.Now}
}

// A block the Lab could not tell ("unclear": asked and downloaded whole at
// once) passes through; a word Peer Map does not know is still dropped.
func TestSiblingReconUnclear(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, c := range []struct{ in, want string }{{"unclear", "unclear"}, {"sideways", ""}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"hash":"00beef","height":967724,"detectedAt":%d,"recon":%q}`, now.UnixMilli(), c.in)
		}))
		s := &siblingCheck{url: srv.URL + "/api/health", blockURL: srv.URL + "/api/blocks/latest", client: srv.Client(), every: time.Minute, now: func() time.Time { return now }}
		got := s.latest(context.Background())
		srv.Close()
		if got == nil || got.Recon != c.want {
			t.Errorf("recon %q: got %+v", c.in, got)
		}
	}
}
