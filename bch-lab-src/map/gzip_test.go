package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

// Text comes packed when the browser takes gzip and plain when it does not;
// an image and a range request are never packed.
func TestGzipped(t *testing.T) {
	h := newHandler(&source{now: time.Now, fetch: mockPeers}, offSibling())
	get := func(path string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	gz := map[string]string{"Accept-Encoding": "gzip, deflate, br"}

	rec := get("/api/peers", gz)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("peer list not packed: %v", rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var snap snapshot
	if err := json.Unmarshal(body, &snap); err != nil || len(snap.Peers) == 0 {
		t.Fatalf("packed peer list does not unpack to peers: %v", err)
	}

	for _, path := range []string{"/", "/app.js", "/style.css", "/world.json"} {
		if rec := get(path, gz); rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Content-Length") != "" {
			t.Errorf("%s: HTTP %d, encoding %q, length %q", path, rec.Code, rec.Header().Get("Content-Encoding"), rec.Header().Get("Content-Length"))
		}
	}
	if rec := get("/api/peers", nil); rec.Header().Get("Content-Encoding") != "" || !json.Valid(rec.Body.Bytes()) {
		t.Error("a browser without gzip got a packed answer")
	}
	if rec := get("/apple-touch-icon.png", gz); rec.Header().Get("Content-Encoding") != "" {
		t.Error("the PNG was packed")
	}
	if rec := get("/style.css", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-9"}); rec.Header().Get("Content-Encoding") != "" {
		t.Error("a range request was packed")
	}
}
