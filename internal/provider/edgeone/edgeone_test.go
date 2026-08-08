package edgeone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// memServer is a minimal in-memory implementation of the section-7
// edge-function protocol used by the provider tests.
type memServer struct {
	mu      sync.Mutex
	token   string
	store   map[string][]byte
	lastKey string
	puts    int
}

func newMemServer(token string) *memServer {
	return &memServer{token: token, store: map[string][]byte{}}
}

func (s *memServer) handler(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		body, ok := s.store[key]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		w.Write(body)
	case http.MethodPut:
		if r.Header.Get("x-token") != s.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.store[key] = body
		s.lastKey = key
		s.puts++
		s.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"url":"%s","size":%d}`, "http://"+r.Host+r.URL.Path, len(body))
	case http.MethodDelete:
		if r.Header.Get("x-token") != s.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s.mu.Lock()
		delete(s.store, key)
		s.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func startMemServer(t *testing.T, token string) (*httptest.Server, *memServer) {
	t.Helper()
	s := newMemServer(token)
	ts := httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(ts.Close)
	return ts, s
}

func TestSafeKeyPreservesSafeInput(t *testing.T) {
	inputs := []string{"a_txt", "sub_2f", "ABC_123", strings.Repeat("y", 512)}
	for _, in := range inputs {
		got, err := safeKey(in)
		if err != nil {
			t.Fatalf("safeKey(%q): %v", in, err)
		}
		if got != in {
			t.Errorf("safeKey(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestSafeKeyEncodesUnsafeBytes(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a.txt", "s_a_2etxt"},
		{"sub/a.txt", "s_sub_2fa_2etxt"},
		// "订阅.txt": every UTF-8 byte becomes _xx, '.' becomes _2e.
		{"Sub_Dir-2.txt", "s_Sub_Dir_2d2_2etxt"},
	}
	for _, tt := range tests {
		got, err := safeKey(tt.in)
		if err != nil {
			t.Fatalf("safeKey(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("safeKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if !isSafe(got) {
			t.Errorf("safeKey(%q) = %q contains bytes outside [A-Za-z0-9_]", tt.in, got)
		}
	}
}

func TestSafeKeyEmpty(t *testing.T) {
	if _, err := safeKey(""); err == nil {
		t.Fatal("safeKey(\"\") should fail")
	}
}

func TestSafeKeyHashesOverlongNames(t *testing.T) {
	in := strings.Repeat("x", 513) // safe characters but exceeds 512 bytes
	got, err := safeKey(in)
	if err != nil {
		t.Fatalf("safeKey: %v", err)
	}
	sum := sha256.Sum256([]byte(in))
	want := "s_" + hex.EncodeToString(sum[:])[:64]
	if got != want {
		t.Errorf("safeKey = %q, want %q", got, want)
	}
	again, err := safeKey(in)
	if err != nil {
		t.Fatalf("safeKey(again): %v", err)
	}
	if again != got {
		t.Errorf("safeKey is not deterministic: %q vs %q", got, again)
	}
}

func TestSafeKeyUnicodeTooLongHashes(t *testing.T) {
	in := strings.Repeat("\u8ba2", 200) // 600 UTF-8 bytes
	got, err := safeKey(in)
	if err != nil {
		t.Fatalf("safeKey: %v", err)
	}
	if len(got) != 66 || !strings.HasPrefix(got, "s_") {
		t.Errorf("safeKey = %q (len %d), want 66-char s_ hash", got, len(got))
	}
}

func TestEdgeOneKVRoundTrip(t *testing.T) {
	ts, srv := startMemServer(t, "sekrit")
	tgt, err := newKV(cloudsync.TargetConfig{
		Name:           "kv",
		BaseURL:        ts.URL,
		Token:          "sekrit",
		FilenamePrefix: "sub/",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	name := "\u76ee\u5f55/\u8ba2\u9605 1.txt" // 目录/订阅 1.txt
	data := []byte("edgeone-kv payload")
	url, err := cloudsync.Push(ctx, tgt, name, data)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	wantKey, err := safeKey("sub/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if srv.lastKey != wantKey {
		t.Errorf("server key = %q, want %q", srv.lastKey, wantKey)
	}
	if !strings.HasSuffix(url, "/"+wantKey) {
		t.Errorf("url = %q, want suffix /%s", url, wantKey)
	}
	if srv.puts != 1 {
		t.Errorf("puts = %d, want 1", srv.puts)
	}

	got, err := tgt.Read(ctx, name)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Read = %q, want %q", got, data)
	}

	if err := tgt.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := tgt.Read(ctx, name); !errors.Is(err, cloudsync.ErrNotFound) {
		t.Fatalf("Read after delete = %v, want ErrNotFound", err)
	}
}

func TestEdgeOneKVLongKeyHashes(t *testing.T) {
	ts, srv := startMemServer(t, "sekrit")
	tgt, err := newKV(cloudsync.TargetConfig{
		Name:           "kv",
		BaseURL:        ts.URL,
		Token:          "sekrit",
		FilenamePrefix: "p/",
	})
	if err != nil {
		t.Fatal(err)
	}
	filename := strings.Repeat("x", 512)
	if _, err := cloudsync.Push(context.Background(), tgt, filename, []byte("x")); err != nil {
		t.Fatalf("Push: %v", err)
	}
	wantKey, _ := safeKey("p/" + filename)
	if srv.lastKey != wantKey {
		t.Errorf("server key = %q, want %q", srv.lastKey, wantKey)
	}
	if len(wantKey) != 66 {
		t.Errorf("hashed key length = %d, want 66", len(wantKey))
	}
}

func TestEdgeOneBlobRoundTripKeepsUnicodeKey(t *testing.T) {
	ts, srv := startMemServer(t, "sekrit")
	tgt, err := newBlob(cloudsync.TargetConfig{
		Name:           "blob",
		BaseURL:        ts.URL,
		Token:          "sekrit",
		Bucket:         "bucket",
		FilenamePrefix: "sub/",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	name := "\u8ba2\u9605.txt" // 订阅.txt
	wantKey := "bucket/sub/" + name
	data := []byte("edgeone-blob payload")
	if _, err := cloudsync.Push(ctx, tgt, name, data); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if srv.lastKey != wantKey {
		t.Errorf("server key = %q, want %q", srv.lastKey, wantKey)
	}
	got, err := tgt.Read(ctx, name)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Read = %q, want %q", got, data)
	}
}

func TestEdgeOneBlobKeyTooLong(t *testing.T) {
	tgt, err := newBlob(cloudsync.TargetConfig{
		Name:           "blob",
		BaseURL:        "http://127.0.0.1:1",
		Token:          "t",
		Bucket:         "b",
		FilenamePrefix: "p/",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cloudsync.Push(context.Background(), tgt, strings.Repeat("x", 512), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "edgeone-blob key too long") {
		t.Fatalf("err = %v, want edgeone-blob key-too-long error", err)
	}
}
