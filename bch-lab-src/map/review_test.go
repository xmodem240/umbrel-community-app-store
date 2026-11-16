package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Found in an outside review (07.11.2026): a "wallet" that offers what no
// wallet can is Disguised, and the "relays no blocks" its name gave it goes
// with the name.
func TestFalseWalletKeepsNoRelayOnlyWhenOfferingNothing(t *testing.T) {
	p := convert([]rawPeer{{Addr: "198.51.100.7:50000", ConnectionType: "inbound", Inbound: true,
		Subver: "/bitcoinj:0.16.3/Bitcoin Wallet:10.24/", Version: 70016, ServicesNames: []string{"NETWORK", "WITNESS"}}})[0]
	if p.Kind != fakeWalletKind {
		t.Fatalf("kind %q, want %q", p.Kind, fakeWalletKind)
	}
	if p.NoRelay {
		t.Errorf("a disguised peer offering NETWORK must not be marked as never relaying blocks")
	}
	w := convert([]rawPeer{{Addr: "198.51.100.8:50000", ConnectionType: "inbound", Inbound: true,
		Subver: "/bitcoinj:0.16.3/Bitcoin Wallet:10.24/", Version: 70012, ServicesNames: []string{"WITNESS"}}})[0]
	if w.Kind != "Wallet" || !w.NoRelay {
		t.Errorf("a real-looking wallet stays a wallet that relays no blocks: %q %v", w.Kind, w.NoRelay)
	}
}

// The Libre Relay bit does not outweigh a protocol version the claimed Core
// could never send.
func TestLibreRelayBitWithImpossibleProtocolIsNotLibreRelay(t *testing.T) {
	p := convert([]rawPeer{{Addr: "198.51.100.9:50000", ConnectionType: "inbound", Inbound: true,
		Subver: "/Satoshi:31.0.0/", Version: 70001, ServicesNames: []string{"NETWORK", "WITNESS", libreRelayService}}})[0]
	if p.Kind == "Node (Libre Relay)" {
		t.Errorf("kind %q despite protocol 70001", p.Kind)
	}
}

func TestSharedFetchRunsOnceForEveryoneWaiting(t *testing.T) {
	f := &sharedFetch{}
	var runs atomic.Int32
	release := make(chan struct{})
	fn := func() snapshot { runs.Add(1); <-release; return snapshot{} }
	var calls []*fetchCall
	for i := 0; i < 500; i++ {
		calls = append(calls, f.join(fn))
	}
	close(release)
	var wg sync.WaitGroup
	for _, c := range calls {
		wg.Add(1)
		go func() { defer wg.Done(); <-c.done }()
	}
	wg.Wait()
	if n := runs.Load(); n != 1 {
		t.Errorf("%d fetches for 500 requests, want 1", n)
	}
	// Once done, the next request starts a fresh one.
	c := f.join(func() snapshot { runs.Add(1); return snapshot{} })
	select {
	case <-c.done:
	case <-time.After(time.Second):
		t.Fatal("second fetch never finished")
	}
	if n := runs.Load(); n != 2 {
		t.Errorf("runs %d, want 2", n)
	}
}

func TestAcceptsGzip(t *testing.T) {
	for h, want := range map[string]bool{
		"gzip": true, "gzip, deflate, br": true, "br;q=1.0, gzip;q=0.8": true, "*": true,
		"gzip;q=0": false, "gzip; q=0.000": false, "identity": false, "": false, "deflate": false,
	} {
		if got := acceptsGzip(h); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", h, got, want)
		}
	}
}
