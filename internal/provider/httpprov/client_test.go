package httpprov

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cloudsync/internal/cloudsync"
)

// kvServer is an in-memory implementation of the section-7 edge-function protocol:
//
//	GET    /<key>                 -> 200 raw body (public, no token)
//	PUT    /<key> x-token: <tok>  -> 200 {"url","size","key"}
//	DELETE /<key> x-token: <tok>  -> 200 {"ok":true}
type kvServer struct {
	mu        sync.Mutex
	token     string
	store     map[string][]byte
	failPut   int // remaining synthetic 500s on PUT
	stale     int // serve this many stale GET responses
	puts      int
	gets      int
	deletes   int
	lastPath  string
	lastQuery string
	lastToken string
}

func newKVServer(token string) *kvServer {
	return &kvServer{token: token, store: map[string][]byte{}}
}

func (s *kvServer) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.lastPath = r.URL.Path
	s.lastQuery = r.URL.RawQuery
	s.lastToken = r.Header.Get("x-token")
	s.mu.Unlock()

	key := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		s.gets++
		var body []byte
		ok := false
		if s.stale > 0 {
			s.stale--
			body, ok = []byte("stale"), true
		} else {
			body, ok = s.store[key]
		}
		s.mu.Unlock()
		if !ok {
			writeJSON(w, 404, map[string]any{"error": "not found"})
			return
		}
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		w.Write(body)
	case http.MethodPut, http.MethodPost:
		s.mu.Lock()
		s.puts++
		fail := s.failPut > 0
		if fail {
			s.failPut--
		}
		s.mu.Unlock()
		if fail {
			writeJSON(w, 500, map[string]any{"error": "boom"})
			return
		}
		if !s.authorized(r) {
			writeJSON(w, 403, map[string]any{"error": "forbidden: token=" + r.Header.Get("x-token")})
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "bad body"})
			return
		}
		s.mu.Lock()
		s.store[key] = body
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{
			"url":  "http://" + r.Host + r.URL.Path,
			"size": len(body),
			"key":  key,
		})
	case http.MethodDelete:
		s.mu.Lock()
		s.deletes++
		s.mu.Unlock()
		if !s.authorized(r) {
			writeJSON(w, 403, map[string]any{"error": "forbidden"})
			return
		}
		s.mu.Lock()
		delete(s.store, key)
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func (s *kvServer) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	return r.Header.Get("x-token") == s.token
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func newTestClient(t *testing.T, ts *httptest.Server, cfg Config) *Client {
	t.Helper()
	if cfg.BaseURL == "" {
		cfg.BaseURL = ts.URL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = ts.Client()
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPushReadDeleteRoundTrip(t *testing.T) {
	srv := newKVServer("sekrit")
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	defer ts.Close()
	c := newTestClient(t, ts, Config{Name: "t", Token: "sekrit"})
	ctx := context.Background()

	url, err := c.Push(ctx, "sub/a.txt", []byte("hello\nworld\n"))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !strings.HasSuffix(url, "/sub/a.txt") {
		t.Errorf("url = %q", url)
	}
	// The write token must travel in a header only.
	if srv.lastToken != "sekrit" {
		t.Errorf("server saw x-token %q", srv.lastToken)
	}
	if srv.lastQuery != "" {
		t.Errorf("token leaked into query string: %q", srv.lastQuery)
	}

	got, err := c.Read(ctx, "sub/a.txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "hello\nworld\n" {
		t.Errorf("Read = %q", got)
	}

	if err := c.Delete(ctx, "sub/a.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Read(ctx, "sub/a.txt"); !errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
	if srv.deletes != 1 {
		t.Errorf("deletes = %d, want 1", srv.deletes)
	}
}

func TestPushRequiresToken(t *testing.T) {
	srv := newKVServer("sekrit")
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	defer ts.Close()
	c := newTestClient(t, ts, Config{Name: "t"})
	if _, err := c.Push(context.Background(), "a", []byte("x")); !errors.Is(err, cloudsync.ErrTokenRequired) {
		t.Fatalf("err = %v, want ErrTokenRequired", err)
	}
	if err := c.Delete(context.Background(), "a"); !errors.Is(err, cloudsync.ErrTokenRequired) {
		t.Fatalf("delete err = %v, want ErrTokenRequired", err)
	}
	if srv.puts != 0 {
		t.Errorf("puts = %d, want 0 (no request should be sent)", srv.puts)
	}
}

func TestPushUnauthorized(t *testing.T) {
	srv := newKVServer("sekrit")
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	defer ts.Close()
	c := newTestClient(t, ts, Config{Name: "t", Token: "wrong"})
	if _, err := c.Push(context.Background(), "a", []byte("x")); !errors.Is(err, cloudsync.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if err := c.Delete(context.Background(), "a"); !errors.Is(err, cloudsync.ErrUnauthorized) {
		t.Fatalf("delete err = %v, want ErrUnauthorized", err)
	}
}

func TestPushRetriesTransient500(t *testing.T) {
	srv := newKVServer("sekrit")
	srv.failPut = 2
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	defer ts.Close()
	c := newTestClient(t, ts, Config{Name: "t", Token: "sekrit", MaxRetries: 3})
	url, err := c.Push(context.Background(), "a.txt", []byte("payload"))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if url == "" {
		t.Error("want non-empty url")
	}
	if srv.puts != 3 {
		t.Errorf("puts = %d, want 3 (2 failures + success)", srv.puts)
	}
}

func TestReadNotFound(t *testing.T) {
	srv := newKVServer("sekrit")
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	defer ts.Close()
	c := newTestClient(t, ts, Config{Name: "t", Token: "sekrit"})
	if _, err := c.Read(context.Background(), "nope.txt"); !errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestKeyForTransform(t *testing.T) {
	srv := newKVServer("sekrit")
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	defer ts.Close()
	c := newTestClient(t, ts, Config{
		Name:  "t",
		Token: "sekrit",
		KeyFor: func(f string) (string, error) {
			return "pre_" + f, nil
		},
	})
	url, err := c.Push(context.Background(), "a.txt", []byte("x"))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !strings.Contains(url, "/pre_a.txt") {
		t.Errorf("url = %q, want pre_a.txt path", url)
	}
	if got, err := c.Read(context.Background(), "a.txt"); err != nil || string(got) != "x" {
		t.Errorf("Read = %q, %v", got, err)
	}
}

func TestVerifyReadBackToleratesStale(t *testing.T) {
	srv := newKVServer("sekrit")
	srv.stale = 1 // first GET after push returns stale content
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	defer ts.Close()
	c := newTestClient(t, ts, Config{Name: "t", Token: "sekrit", Verify: true})
	if _, err := cloudsync.Push(context.Background(), c, "a.txt", []byte("final")); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if srv.gets < 2 {
		t.Errorf("gets = %d, want >= 2 (stale then fresh)", srv.gets)
	}
}

func TestPushLargePayloadOver1000Lines(t *testing.T) {
	srv := newKVServer("sekrit")
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	defer ts.Close()
	c := newTestClient(t, ts, Config{Name: "t", Token: "sekrit", Verify: true})

	var b strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "line %04d 0123456789abcdef\n", i)
	}
	data := []byte(b.String())

	url, err := cloudsync.Push(context.Background(), c, "sub/big.txt", data)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !strings.HasSuffix(url, "/sub/big.txt") {
		t.Errorf("url = %q", url)
	}
	got, err := c.Read(context.Background(), "sub/big.txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("large payload read-back differs (%d vs %d bytes)", len(got), len(data))
	}
}
