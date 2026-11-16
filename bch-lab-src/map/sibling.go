package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// The other half of the pair is Bitcoin Lab: a separate app, installed on its
// own, which measures which of these peers actually delivers each block first.
// When it is installed on the same node, this app offers a link to it - both
// for the dashboard as a whole and for a single peer.
//
// Whether it is there is a question for this process, not for the browser. The
// dashboard runs under a Content-Security-Policy with connect-src 'self', which
// is worth keeping, and a browser could only ever find out whether something
// answers on a port. From inside the umbrel network the question is the real
// one: does the container exist. It is reachable by name, exactly like Bitcoin
// Core is.
//
// A miss is the normal case - most people will install one app and not the
// other - so a failure is silent and simply means no link.
const siblingDefault = "http://bitcoinlab-node_dashboard_1:8788/api/health"

// How long a delivered block is worth pointing at. Long enough to notice on a
// dashboard somebody just opened, short enough that a star never turns into
// decoration: after two minutes the block is history and the map goes back to
// showing connections.
const blockMarkFor = 2 * time.Minute

// The most of /api/blocks/latest this reads. The reply is a few kilobytes;
// the limit is a guard against a neighbour gone wrong, not a budget.
const blockReplyLimit = 64 << 10

const siblingMissEvery = 30 * time.Second

type siblingCheck struct {
	url      string
	blockURL string
	client   *http.Client
	every    time.Duration
	now      func() time.Time

	mu        sync.Mutex
	checkedAt time.Time
	present   bool

	lastProblem     string // the last reply problem logged, so it is logged once
	blockAt         time.Time
	blockDetectedAt int64
	block           *lastBlock
}

type stratumRace struct {
	CreatedAt int64          `json:"created_at,omitempty"`
	Entries   []stratumEntry `json:"entries,omitempty"`
	// When this node's Core had the block, counted from the first new job any
	// pool sent here - the race's zero. Both instants come from Bitcoin Lab,
	// taken on one machine by one clock: the ZMQ announcement and the first
	// notify. Negative when Core had the block before any pool sent a job,
	// which does happen. A pointer because zero is a real answer.
	//
	// When the block was found cannot be known - the header's time is set by
	// the miner, in whole seconds - which is exactly why the first job is the
	// zero: it is the earliest moment the block is visible from here.
	CoreMs *int64 `json:"core_ms,omitempty"`
}

// One stop of the typical route: its median and over how many blocks.
type medianStop struct {
	Ms float64 `json:"ms"`
	N  int     `json:"n"`
}

type routeMedian struct {
	Blocks   int         `json:"blocks"`
	Core     *medianStop `json:"core,omitempty"`
	Peer     *medianStop `json:"peer,omitempty"`
	Template *medianStop `json:"template,omitempty"`
	Own      *medianStop `json:"own,omitempty"`
	OwnLabel string      `json:"own_label,omitempty"`
	// Transactions per template, the median over the same blocks.
	Tx *medianStop `json:"tx,omitempty"`
}

// How blocks were rebuilt from their compact form over a day or a week, as
// Bitcoin Lab counted them: from the mempool alone, after asking for missing
// transactions (and what came back on average), or downloaded whole.
type reconWindow struct {
	Blocks        int    `json:"blocks"`
	Mempool       int    `json:"mempool"`
	Asked         int    `json:"asked"`
	Full          int    `json:"full"`
	AskedAvgBytes *int64 `json:"asked_avg_bytes,omitempty"`
}

type stratumEntry struct {
	Label     string   `json:"label"`
	Own       bool     `json:"own,omitempty"` // a pool the owner added, not one of the public ones
	LatencyMs *float64 `json:"latency_ms"`    // null means it sent nothing in time
	Rank      *int     `json:"rank"`
	Miss      bool     `json:"miss,omitempty"`
}

// What the neighbour says about the newest block. Its own fields are the
// short pool name, the full one, and the addresses Bitcoin Lab credited with
// delivering it - the same strings Core reports here, ports and all, so they
// match this app's peers without any guessing.
type lastBlock struct {
	Hash       string   `json:"hash,omitempty"`
	Height     int64    `json:"height,omitempty"`
	Pool       string   `json:"pool,omitempty"`
	PoolName   string   `json:"pool_name,omitempty"`
	PoolTag    string   `json:"pool_tag,omitempty"`
	PoolSource string   `json:"pool_source,omitempty"`
	FirstPeers []string `json:"first_peers,omitempty"`
	// How many peers were connected when it arrived - the number the one that
	// delivered it was up against.
	Eligible int `json:"eligible,omitempty"`
	// The same block from the mining side: which pool turned it into fresh
	// work first, and how far behind the others were. Absent when Bitcoin Lab
	// recorded no race for it, which is the normal case with Stratum Race off.
	Stratum *stratumRace `json:"stratum,omitempty"`
	// The route's inner stops, measured by Bitcoin Lab at the moment the
	// block arrived: how long after the announcement Core had a new block
	// template ready, and the delivering peer's lowest ping from the snapshot
	// that credited it. Absent on blocks recorded before the Lab measured them.
	TemplateMs  *float64 `json:"template_ms,omitempty"`
	FirstPingMs *float64 `json:"first_ping_ms,omitempty"`
	// Whether that peer was one of Core's high-bandwidth peers just before
	// the block (1 or 0): it decides how many pings the hop into Core takes.
	// Absent where Bitcoin Lab did not know, or is older than this.
	FirstHb *int `json:"first_hb,omitempty"`
	// How many transactions that template carried: a pool spends time on
	// every one, so this is why one block's job is slower than the next.
	TemplateTx *int `json:"template_tx,omitempty"`
	// The same route for the typical block, the median over the last hundred.
	RouteMedian *routeMedian `json:"route_median,omitempty"`
	// How Core put this block together from its compact form: "mempool",
	// "asked" (with the bytes that came back) or "full". Absent where Bitcoin
	// Lab could not tell, or is older than this.
	Recon             string `json:"recon,omitempty"`
	ReconMissingBytes *int64 `json:"recon_missing_bytes,omitempty"`
	// The block's size in bytes, to show what was missing as a share of it.
	BlockSize *int64 `json:"block_size,omitempty"`
	// The same over the last hundred blocks, the card's "typical" window.
	ReconLast100 *reconWindow `json:"recon_last100,omitempty"`
	// Every address Bitcoin Lab has ever credited with delivering a block
	// first. The tables tint those rows.
	DeliveredEver []string `json:"delivered_ever,omitempty"`
	// Age at the moment this snapshot was built, from the clock both apps
	// share - the browser adds however long its copy has been sitting there
	// rather than comparing two clocks that may disagree.
	AgeMs int64 `json:"age_ms"`
	// How long a browser should keep the mark, so the rule lives in one place.
	MarkForMs int64 `json:"mark_for_ms"`
}

func newSiblingCheck() *siblingCheck {
	url := os.Getenv("PEERMAP_SIBLING_HEALTH_URL")
	if url == "" {
		url = siblingDefault
	}
	if url != "off" && !strings.HasSuffix(url, "/api/health") {
		// The block card's address is built from this one.
		log.Printf("PEERMAP_SIBLING_HEALTH_URL %q does not end in /api/health: the block card will ask %s", url, strings.TrimSuffix(url, "/api/health")+"/api/blocks/latest")
	}
	return &siblingCheck{
		url:      url,
		blockURL: strings.TrimSuffix(url, "/api/health") + "/api/blocks/latest",
		now:      time.Now,
		client:   &http.Client{Timeout: 2 * time.Second},
		// An app is installed or removed by hand, minutes apart at the very
		// fastest. Asking once every five minutes is already generous, and it
		// keeps a missing neighbour from costing a request per dashboard poll.
		every: 5 * time.Minute,
	}
}

// clock is s.now, or the wall clock for a check built without one.
func (s *siblingCheck) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// installed reports whether Bitcoin Lab answered recently. Never blocks longer
// than the client timeout, and only that rarely.
func (s *siblingCheck) installed(ctx context.Context) bool {
	if s.url == "off" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A miss is asked again sooner, as Bitcoin Lab does: updated together,
	// the neighbour may still be starting when this app asks.
	wait := s.every
	if !s.present {
		wait = siblingMissEvery
	}
	if !s.checkedAt.IsZero() && s.clock().Sub(s.checkedAt) < wait {
		return s.present
	}
	// The probe is this app's, not the browser's: tied to the incoming request
	// it was cancelled whenever a dashboard went away mid-poll, and that
	// cancellation was then remembered as "no neighbour" for the next five
	// minutes - the link vanished for everyone. The lock is held across the
	// call, so pollers arriving meanwhile still share this one probe.
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodGet, s.url, nil)
	if err != nil {
		s.checkedAt, s.present = s.clock(), false
		return false
	}
	resp, err := s.client.Do(req)
	if err != nil {
		// A neighbour that is not there is the normal case and is an answer:
		// remembered, or a missing app costs a request per dashboard poll.
		s.checkedAt, s.present = s.clock(), false
		return false
	}
	defer resp.Body.Close()
	s.checkedAt, s.present = s.clock(), resp.StatusCode == http.StatusOK
	return s.present
}

// latest returns the newest block the neighbour knows about, or nil when
// there is no neighbour, it did not answer, or it has not seen a block yet.
//
// Asked at most once per poll interval and never on the page's critical path:
// a slow or missing Bitcoin Lab costs this app nothing but a missing star.
func (s *siblingCheck) latest(ctx context.Context) *lastBlock {
	if s.url == "off" || !s.installed(ctx) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if !s.blockAt.IsZero() && now.Sub(s.blockAt) < interval {
		return s.withAge(s.block, now)
	}
	// A neighbour that answers badly has answered: no block for this window,
	// so a broken Bitcoin Lab costs one request per interval and no more.
	miss := func() *lastBlock {
		s.blockAt, s.block = now, nil
		return nil
	}
	// Detached from the browser request for the same reason as the probe
	// above: a dashboard that goes away must not blank the block card for
	// every other dashboard until the window runs out.
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodGet, s.blockURL, nil)
	if err != nil {
		return miss()
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return miss()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return miss()
	}
	// Bitcoin Lab's own shape, which is not this app's: decoded here and
	// handed on in this app's vocabulary.
	var got struct {
		Hash       string   `json:"hash"`
		Height     int64    `json:"height"`
		DetectedAt int64    `json:"detectedAt"`
		Pool       string   `json:"pool"`
		PoolName   string   `json:"poolName"`
		PoolTag    string   `json:"poolTag"`
		PoolSource string   `json:"poolSource"`
		FirstPeers []string `json:"firstPeers"`
		Eligible   int      `json:"eligible"`
		Stratum    *struct {
			CreatedAt int64 `json:"createdAt"`
			Entries   []struct {
				Label     string   `json:"label"`
				Own       bool     `json:"own"`
				LatencyMs *float64 `json:"latencyMs"`
				Rank      *int     `json:"rank"`
				Miss      bool     `json:"miss"`
			} `json:"entries"`
		} `json:"stratum"`
		TemplateMs    *float64 `json:"templateMs"`
		TemplateTx    *int     `json:"templateTx"`
		FirstPingMs   *float64 `json:"firstPingMs"`
		FirstHb       *int     `json:"firstHb"`
		DeliveredEver []string `json:"deliveredEver"`
		Recon         *string  `json:"recon"`
		ReconMissing  *int64   `json:"reconMissingBytes"`
		BlockSize     *int64   `json:"blockSize"`
		ReconLast100  *struct {
			Blocks, Mempool, Asked, Full int
			AskedAvgBytes                *int64 `json:"askedAvgBytes"`
		} `json:"reconLast100"`
		RouteMedian *struct {
			Blocks   int         `json:"blocks"`
			Core     *medianStop `json:"core"`
			Peer     *medianStop `json:"peer"`
			Template *medianStop `json:"template"`
			Own      *medianStop `json:"own"`
			OwnLabel string      `json:"ownLabel"`
			Tx       *medianStop `json:"tx"`
		} `json:"routeMedian"`
	}
	// Read one byte past the limit, so a reply that does not fit is told apart
	// from one that merely ends there. Either way there is no card to show -
	// a truncated reply is not one to trust - but a reply too large to read
	// used to cost the card without a word, so the log now says which it was,
	// once, rather than nothing on every poll.
	body, err := io.ReadAll(io.LimitReader(resp.Body, blockReplyLimit+1))
	switch {
	case err != nil:
		s.problem(fmt.Sprintf("reading Bitcoin Lab's latest block failed: %v", err))
		return miss()
	case len(body) > blockReplyLimit:
		s.problem("Bitcoin Lab's latest block reply is larger than 64 KB - the block card is left out")
		return miss()
	}
	if err := json.Unmarshal(body, &got); err != nil || got.DetectedAt == 0 {
		if err != nil {
			s.problem(fmt.Sprintf("Bitcoin Lab's latest block reply could not be read: %v", err))
		}
		return miss()
	}
	s.problem("")
	age := now.UnixMilli() - got.DetectedAt
	if age < 0 {
		age = 0
	}
	var race *stratumRace
	if got.Stratum != nil {
		race = &stratumRace{CreatedAt: got.Stratum.CreatedAt}
		if got.Stratum.CreatedAt > 0 {
			ms := got.DetectedAt - got.Stratum.CreatedAt
			race.CoreMs = &ms
		}
		for _, e := range got.Stratum.Entries {
			race.Entries = append(race.Entries, stratumEntry{
				Label: e.Label, Own: e.Own, LatencyMs: e.LatencyMs, Rank: e.Rank, Miss: e.Miss,
			})
		}
	}
	var med *routeMedian
	if m := got.RouteMedian; m != nil && m.Blocks > 0 {
		med = &routeMedian{Blocks: m.Blocks, Core: m.Core, Peer: m.Peer, Template: m.Template, Own: m.Own, OwnLabel: m.OwnLabel, Tx: m.Tx}
	}
	window := func(blocks, mempool, asked, full int, avg *int64) *reconWindow {
		if blocks <= 0 {
			return nil
		}
		return &reconWindow{Blocks: blocks, Mempool: mempool, Asked: asked, Full: full, AskedAvgBytes: avg}
	}
	var reconLast *reconWindow
	if l := got.ReconLast100; l != nil {
		reconLast = window(l.Blocks, l.Mempool, l.Asked, l.Full, l.AskedAvgBytes)
	}
	recon := ""
	if got.Recon != nil {
		switch *got.Recon {
		case "mempool", "asked", "full", "unclear":
			recon = *got.Recon
		}
	}
	s.blockAt = now
	s.block = &lastBlock{
		Hash:              got.Hash,
		Height:            got.Height,
		Pool:              got.Pool,
		PoolName:          got.PoolName,
		PoolTag:           got.PoolTag,
		PoolSource:        got.PoolSource,
		FirstPeers:        got.FirstPeers,
		Eligible:          got.Eligible,
		Stratum:           race,
		TemplateMs:        got.TemplateMs,
		TemplateTx:        got.TemplateTx,
		FirstPingMs:       got.FirstPingMs,
		FirstHb:           got.FirstHb,
		RouteMedian:       med,
		Recon:             recon,
		ReconMissingBytes: got.ReconMissing,
		BlockSize:         got.BlockSize,
		ReconLast100:      reconLast,
		DeliveredEver:     got.DeliveredEver,
		AgeMs:             age,
		MarkForMs:         blockMarkFor.Milliseconds(),
	}
	// Keep the arrival instant rather than the age, so a cached copy handed
	// out nine seconds later does not claim to be nine seconds younger.
	s.blockDetectedAt = got.DetectedAt
	return s.block
}

// problem logs a reply problem once, and forgets it when the reply is fine
// again. Called with s.mu held.
func (s *siblingCheck) problem(p string) {
	if p != "" && p != s.lastProblem {
		log.Printf("%s", p)
	}
	s.lastProblem = p
}

func (s *siblingCheck) withAge(b *lastBlock, now time.Time) *lastBlock {
	if b == nil {
		return nil
	}
	out := *b
	out.AgeMs = now.UnixMilli() - s.blockDetectedAt
	if out.AgeMs < 0 {
		out.AgeMs = 0
	}
	return &out
}
