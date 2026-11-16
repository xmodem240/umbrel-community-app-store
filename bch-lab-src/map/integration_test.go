//go:build integration

// Run with a bitcoind binary on PATH:
//
//	go test -tags integration -run TestAgainstBitcoinCore -v -count=1 .
//
// CI does this on release tags (see .github/workflows/ci.yml).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
)

type node struct {
	p2p, rpc int
	url      string
	cmd      *exec.Cmd
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func call(n *node, method string, params ...any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "1.0", "id": "t", "method": method, "params": params})
	req, _ := http.NewRequest("POST", n.url, bytes.NewReader(body))
	req.SetBasicAuth("u", "p")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("HTTP %d: %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %d %s", method, out.Error.Code, out.Error.Message)
	}
	return out.Result, nil
}

func startNode(t *testing.T) *node {
	n := &node{p2p: freePort(t), rpc: freePort(t)}
	n.url = fmt.Sprintf("http://127.0.0.1:%d/", n.rpc)
	n.cmd = exec.Command("bitcoind",
		"-regtest", "-datadir="+t.TempDir(), "-server=1", "-listen=1",
		"-bind=127.0.0.1", fmt.Sprintf("-port=%d", n.p2p), fmt.Sprintf("-rpcport=%d", n.rpc),
		"-rpcuser=u", "-rpcpassword=p", "-dnsseed=0", "-fixedseeds=0", "-discover=0",
		"-listenonion=0", "-printtoconsole=0")
	var stderr bytes.Buffer
	n.cmd.Stderr = &stderr
	if err := n.cmd.Start(); err != nil {
		t.Fatalf("start bitcoind: %v", err)
	}
	t.Cleanup(func() {
		call(n, "stop")
		done := make(chan struct{})
		go func() { n.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			n.cmd.Process.Kill()
		}
	})
	deadline := time.Now().Add(60 * time.Second)
	for {
		_, err := call(n, "getnetworkinfo")
		if err == nil {
			return n
		}
		if time.Now().After(deadline) {
			t.Fatalf("bitcoind RPC never came up: %v; stderr: %s", err, stderr.String())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func fetchSnapshot(t *testing.T, n *node) snapshot {
	rpc := &rpcClient{url: n.url, user: "u", pass: "p", http: http.DefaultClient}
	src := &source{now: time.Now, fetch: rpc.getPeerInfo}
	rec := httptest.NewRecorder()
	newHandler(src, offSibling()).ServeHTTP(rec, httptest.NewRequest("GET", "/api/peers", nil))
	var s snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Error != "" {
		t.Fatalf("snapshot error: %s", s.Error)
	}
	return s
}

func TestAgainstBitcoinCore(t *testing.T) {
	a, b, c, d := startNode(t), startNode(t), startNode(t), startNode(t)

	// One connection of each kind the map separates, all opened by A.
	if _, err := call(a, "addnode", fmt.Sprintf("127.0.0.1:%d", b.p2p), "onetry"); err != nil {
		t.Fatal(err)
	}
	// addconnection is a regtest-only test RPC; its third argument (v2transport)
	// exists since Core 26.
	if _, err := call(a, "addconnection", fmt.Sprintf("127.0.0.1:%d", c.p2p), "outbound-full-relay", false); err != nil {
		t.Fatal(err)
	}
	if _, err := call(a, "addconnection", fmt.Sprintf("127.0.0.1:%d", d.p2p), "block-relay-only", false); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{"manual": false, "outbound-full-relay": false, "block-relay-only": false}
	deadline := time.Now().Add(30 * time.Second)
	for {
		raw, err := call(a, "getpeerinfo")
		if err != nil {
			t.Fatal(err)
		}
		var peers []rawPeer
		json.Unmarshal(raw, &peers)
		for k := range want {
			want[k] = false
		}
		for _, p := range peers {
			if _, ok := want[p.ConnectionType]; ok {
				want[p.ConnectionType] = true
			}
		}
		if want["manual"] && want["outbound-full-relay"] && want["block-relay-only"] {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connections never all appeared on A: %v (getpeerinfo: %s)", want, raw)
		}
		time.Sleep(250 * time.Millisecond)
	}

	sa := fetchSnapshot(t, a)
	count := map[string]int{}
	for _, p := range sa.Peers {
		count[p.Group]++
		t.Logf("A sees %s type=%s network=%s group=%s cc=%q", p.Addr, p.Type, p.Network, p.Group, p.Country)
		if p.Country != "" {
			t.Errorf("localhost peer %s was placed in %q", p.Addr, p.Country)
		}
	}
	if count["manual"] != 1 || count["outbound"] != 2 || count["inbound"] != 0 {
		t.Errorf("A: groups %v, want manual=1 outbound=2 inbound=0", count)
	}

	// B, C and D each see A's connection as inbound.
	for name, n := range map[string]*node{"B": b, "C": c, "D": d} {
		s := fetchSnapshot(t, n)
		if len(s.Peers) != 1 || s.Peers[0].Group != "inbound" || s.Peers[0].Type != "inbound" {
			t.Errorf("%s: want exactly one inbound peer, got %+v", name, s.Peers)
		}
	}
}
