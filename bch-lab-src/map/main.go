// Peer Map shows the peers of a Bitcoin Core node on a world map, split into
// manual, inbound and outbound connections.
//
// It is built to cost nothing while nobody is looking: there is no background
// loop. Core is asked for getpeerinfo only when the dashboard asks for data,
// and never more than once per interval (10s) no matter how many browsers are
// open - every request inside that window gets the cached answer.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/benunskilled/peer-map/asn"
	"github.com/benunskilled/peer-map/geo"
)

// Version is set at build time with -ldflags "-X main.Version=...".
var Version = "dev"

const interval = 10 * time.Second

//go:embed web
var webFS embed.FS

type config struct {
	port string
	host string // PEERMAP_HOST: the interface to listen on; empty means all
	// Bitcoin Cash Lab: the dashboard hands over the node's peers (already in
	// the shape this app reads), so the node's address and login stay in one
	// place - the Node card - and a change there needs no restart here.
	peersURL string
	mock     bool
	rpcURL   string
	rpcUser  string
	rpcPass  string
}

func loadConfig() (config, error) {
	c := config{port: env("PEERMAP_PORT", "8789"), mock: os.Getenv("PEERMAP_MOCK") == "1"}
	if p, err := strconv.Atoi(c.port); err != nil || p < 1 || p > 65535 {
		return c, fmt.Errorf("PEERMAP_PORT=%q is not a port number", c.port)
	}
	c.host = os.Getenv("PEERMAP_HOST")
	if c.mock {
		return c, nil
	}
	if u := os.Getenv("PEERMAP_PEERS_URL"); u != "" {
		if !strings.HasPrefix(u, "http://") {
			return c, fmt.Errorf("PEERMAP_PEERS_URL=%q must be an internal http:// address", u)
		}
		c.peersURL = u
		return c, nil
	}
	var missing []string
	get := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			missing = append(missing, k)
		}
		return v
	}
	host, port := get("APP_BITCOIN_NODE_IP"), get("APP_BITCOIN_RPC_PORT")
	c.rpcUser, c.rpcPass = get("APP_BITCOIN_RPC_USER"), get("APP_BITCOIN_RPC_PASS")
	if len(missing) > 0 {
		return c, fmt.Errorf("missing environment variable(s) %v (set PEERMAP_MOCK=1 to run with demo data)", missing)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return c, fmt.Errorf("APP_BITCOIN_RPC_PORT=%q is not a port number", port)
	}
	c.rpcURL = "http://" + net.JoinHostPort(host, port) + "/"
	return c, nil
}

// siblingLinkPort is PEERMAP_SIBLING_LINK_PORT when it is a valid port, else 0
// (the page then uses Bitcoin Lab's store port, 8790).
func siblingLinkPort() int {
	p, err := strconv.Atoi(os.Getenv("PEERMAP_SIBLING_LINK_PORT"))
	if err != nil || p < 1 || p > 65535 {
		return 0
	}
	return p
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ---- Bitcoin Core RPC ------------------------------------------------------

// An error with two faces: the detail for the log, and the one line the
// dashboard is allowed to see. Go's *url.Error prints the URL it failed on,
// which is Core's host and port - the node's address on the umbrel network,
// which no browser has any business learning. The dashboard is told only what
// is written here, so an error type nobody anticipated cannot leak a new
// detail by accident.
type shownError struct {
	line string // for the browser; nothing about where the node lives
	err  error  // for the log; nil when the line is the whole story
}

func (e *shownError) Error() string {
	if e.err == nil {
		return e.line
	}
	return e.err.Error()
}

func (e *shownError) Unwrap() error { return e.err }

// shownText is the line the snapshot may carry for err. An error that never
// passed through the RPC client says as little as possible.
func shownText(err error) string {
	var s *shownError
	if errors.As(err, &s) {
		return s.line
	}
	return "could not be asked for its peers"
}

type rpcClient struct {
	url, user, pass string
	peersURL        string // when set, the peers are fetched from here with a GET instead
	http            *http.Client
}

// The dashboard prefixes these lines with "Bitcoin Core: ", so they are
// written to continue that sentence.
func (c *rpcClient) getPeerInfo(ctx context.Context) ([]rawPeer, error) {
	body := []byte(`{"jsonrpc":"1.0","id":"peermap","method":"getpeerinfo","params":[]}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if c.peersURL != "" {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, c.peersURL, nil)
	}
	if err != nil {
		return nil, &shownError{line: "could not be asked for its peers", err: err}
	}
	if c.peersURL == "" {
		req.SetBasicAuth(c.user, c.pass)
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &shownError{line: "unreachable from this app", err: fmt.Errorf("cannot reach the node: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, &shownError{line: "rejected the RPC credentials (401)"}
	}
	var out struct {
		Result []rawPeer `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	// 200 peers are roughly 250 KB of JSON; 32 MB is a guard, not a budget.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		return nil, &shownError{
			line: fmt.Sprintf("sent a reply this app could not read (HTTP %d)", resp.StatusCode),
			err:  fmt.Errorf("unreadable RPC reply (HTTP %d): %w", resp.StatusCode, err),
		}
	}
	if out.Error != nil {
		// Core's own words about the request - "Loading block index...", and
		// the like. Worth showing: it says what to wait for, and unlike a
		// transport error it names nothing about where the node lives.
		return nil, &shownError{line: fmt.Sprintf("RPC error %d: %s", out.Error.Code, out.Error.Message)}
	}
	// A reply without a list is not "no peers": taking it as one replaced the
	// last good list with an empty map. An empty list decodes as [], not nil.
	if out.Result == nil {
		return nil, &shownError{line: "sent no peer list", err: fmt.Errorf("RPC reply without a result (HTTP %d)", resp.StatusCode)}
	}
	return out.Result, nil
}

// ---- cache: at most one RPC call per interval ------------------------------

type snapshot struct {
	Peers     []Peer `json:"peers"`
	FetchedAt int64  `json:"fetched_at"`
	NextIn    int    `json:"next_in"`
	Interval  int    `json:"interval"`
	Error     string `json:"error,omitempty"`
	Demo      bool   `json:"demo,omitempty"`
	Sibling   bool   `json:"sibling,omitempty"` // Bitcoin Lab is installed on this node
	// The port the browser link to Bitcoin Lab uses, when it is not the store's
	// 8790 (PEERMAP_SIBLING_LINK_PORT, for a test store that runs both apps on
	// other ports). Absent means 8790.
	SiblingPort int `json:"sibling_port,omitempty"`
	// The newest block Bitcoin Lab has seen, when it is installed: who mined
	// it, and which peer here delivered it. Absent otherwise, and absent
	// while it has not seen one.
	LastBlock *lastBlock `json:"last_block,omitempty"`
	// Where this node is (a region), so the map can draw the lines to it.
	Home    *Home  `json:"home,omitempty"`
	Version string `json:"version"`
}

type source struct {
	mu      sync.Mutex
	fetch   func(context.Context) ([]rawPeer, error)
	now     func() time.Time
	last    time.Time // when the last answer came in; the window runs from there
	lastErr string    // the last failure logged, so a node that stays down logs once
	snap    snapshot
}

func (s *source) get(ctx context.Context) snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.last.IsZero() || now.Sub(s.last) >= interval {
		// The call belongs to this app, not to the browser request that
		// triggered it: a dashboard that navigates away mid-poll used to
		// cancel the RPC, and the cancellation was then cached as Core's
		// answer - every other open dashboard read an error its node never
		// produced. The lock is still held across the call, so the pollers
		// waiting behind it get this one answer instead of asking again.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		raw, err := s.fetch(cctx)
		cancel()
		now = s.now() // the call can take seconds; the window runs from its end
		// No case for context.Canceled: the call runs on a context detached
		// from the browser's, so only the 20s deadline can end it early, and
		// a node that does not answer within that has answered all the same.
		switch {
		case err != nil:
			// A node that is down has answered all the same. The window holds,
			// so it is asked once per interval and not once per poll.
			s.last = now
			if s.lastErr != err.Error() {
				log.Printf("getpeerinfo failed: %v", err)
				s.lastErr = err.Error()
			}
			s.snap.Error = shownText(err) // keep the last good peer list on screen
		default:
			s.last = now
			s.lastErr = ""
			s.snap.Peers = convertAt(raw, now)
			s.snap.Home = homeOf(raw)
			s.snap.FetchedAt = now.Unix()
			s.snap.Error = ""
		}
	}
	out := s.snap
	out.Interval = int(interval / time.Second)
	out.NextIn = int((interval - now.Sub(s.last) + time.Second - 1) / time.Second)
	if out.NextIn < 1 {
		out.NextIn = 1
	}
	if out.Peers == nil {
		out.Peers = []Peer{}
	}
	return out
}

// ---- HTTP -------------------------------------------------------------------

func newHandler(src *source, sib *siblingCheck) http.Handler {
	static, _ := fs.Sub(webFS, "web")
	files := http.FileServer(http.FS(static))
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "ok\n")
	})
	// One answer in the making at a time, whoever asks: the locks below are
	// held across Core's and Bitcoin Lab's answers, up to 24 seconds, and a
	// browser that gives up meanwhile must not leave a worker of its own
	// behind. Requests that come while one is under way wait for that one.
	fetch := &sharedFetch{}
	mux.HandleFunc("GET /api/peers", func(w http.ResponseWriter, r *http.Request) {
		call := fetch.join(func() snapshot {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			snap := src.get(ctx)
			snap.Sibling = sib.installed(ctx)
			if snap.Sibling {
				snap.SiblingPort = siblingLinkPort()
			}
			snap.LastBlock = sib.latest(ctx)
			return snap
		})
		var snap snapshot
		select {
		case <-call.done:
			snap = call.snap
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(snap)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// no-cache for everything, world.json included: it changes between
		// releases, and a day-long cache kept showing the old map after an update.
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// base-uri and form-action have no fallback: default-src does not cover
		// them, so without these two an injected <base> could re-point every
		// relative URL on the page, and an injected form could post off-site.
		// The dashboard has neither a <base> nor a form, so both are 'none'.
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
		gzipped(mux).ServeHTTP(w, r)
	})
}

// gzipped packs text answers for a browser that takes gzip. The peer list is
// about 270 kB of JSON and comes again every poll; packed it is a fifth of
// that, which counts on a phone away from home. A range request is left
// alone: its offsets would point into the packed bytes.
func gzipped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet || r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		g := &gzipWriter{ResponseWriter: w}
		next.ServeHTTP(g, r)
		if g.gz != nil {
			g.gz.Close()
		}
	})
}

// gzipWriter decides on the status and type the handler set: only a 200 with
// text in it is packed, so a 304 stays empty and a PNG is not packed twice.
type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	started bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.started {
		return
	}
	g.started = true
	h := g.Header()
	if code == http.StatusOK && h.Get("Content-Encoding") == "" && packable(h.Get("Content-Type")) {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		g.gz = gzip.NewWriter(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.started {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func packable(contentType string) bool {
	for _, t := range []string{"text/", "application/json", "application/javascript", "text/javascript", "image/svg+xml"} {
		if strings.HasPrefix(contentType, t) {
			return true
		}
	}
	return false
}

func healthcheck() int {
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + env("PEERMAP_PORT", "8789") + "/api/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck())
		case "version", "-version", "--version":
			fmt.Println(Version)
			return
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [healthcheck|version]\n", os.Args[0])
			os.Exit(2)
		}
	}
	log.SetFlags(0)
	// This build is for Bitcoin Cash: see bch.go.
	bchMode = true

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("peer-map: %v", err)
	}

	src := &source{now: time.Now}
	if cfg.mock {
		src.fetch = mockPeers
		src.snap.Demo = true
	} else {
		rpc := &rpcClient{url: cfg.rpcURL, user: cfg.rpcUser, pass: cfg.rpcPass, peersURL: cfg.peersURL, http: &http.Client{}}
		src.fetch = rpc.getPeerInfo
	}
	src.snap.Version = Version

	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.host, cfg.port),
		Handler:           newHandler(src, newSiblingCheck()),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	regions, v4, v6 := geo.Sizes()
	networks, a4, a6 := asn.Size()
	mode := "rpc " + cfg.rpcURL
	if cfg.peersURL != "" {
		mode = "peers from " + cfg.peersURL
	}
	if cfg.mock {
		mode = "DEMO DATA (PEERMAP_MOCK=1)"
	}
	log.Printf("peer-map %s listening on :%s, %s, geo table %d regions, %d/%d ranges (v4/v6), network table %d operators, %d/%d ranges (v4/v6), the node asked at most every %s and only while the dashboard is open",
		Version, cfg.port, mode, regions, v4, v6, networks, a4, a6, interval)

	ln, err := listenConfig().Listen(context.Background(), "tcp", srv.Addr)
	if err != nil {
		log.Fatalf("peer-map: %v", err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("peer-map: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// listenConfig is how the dashboard's port is opened. Go listeners accept
// Multipath TCP by default on Linux (since Go 1.24); the dashboard only ever
// gets plain connections from Umbrel's app proxy, so it is switched off and
// the kernel's MPTCP code is never reached through this app.
func listenConfig() *net.ListenConfig {
	lc := &net.ListenConfig{}
	lc.SetMultipathTCP(false)
	return lc
}

// sharedFetch runs at most one fetch at a time and hands its result to every
// request that joined while it ran. A request that gives up only stops
// waiting; the fetch finishes for the others, and nothing else is left behind.
type sharedFetch struct {
	mu  sync.Mutex
	cur *fetchCall
}

type fetchCall struct {
	done chan struct{}
	snap snapshot
}

func (f *sharedFetch) join(fn func() snapshot) *fetchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur != nil {
		return f.cur
	}
	c := &fetchCall{done: make(chan struct{})}
	f.cur = c
	go func() {
		c.snap = fn()
		f.mu.Lock()
		f.cur = nil
		f.mu.Unlock()
		close(c.done)
	}()
	return c
}

// acceptsGzip reads Accept-Encoding as a list: "gzip" or "*" counts, unless
// its weight is zero ("gzip;q=0" means: never gzip).
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "gzip" && name != "*" {
			continue
		}
		q := strings.ReplaceAll(strings.ToLower(params), " ", "")
		if q == "q=0" || strings.HasPrefix(q, "q=0.0") && strings.Trim(strings.TrimPrefix(q, "q="), "0.") == "" {
			return false
		}
		return true
	}
	return false
}
