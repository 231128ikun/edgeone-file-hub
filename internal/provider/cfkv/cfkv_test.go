package cfkv

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cloudsync/internal/cloudsync"
)

// kvHandler is a minimal in-memory implementation of the section-7
// edge-function protocol for cfkv tests.
type kvHandler struct {
	mu      sync.Mutex
	token   string
	store   map[string][]byte
	lastKey string
}

func (h *kvHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodGet:
		h.mu.Lock()
		body, ok := h.store[key]
		h.mu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Write(body)
	case http.MethodPut:
		if r.Header.Get("x-token") != h.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		h.store[key] = body
		h.lastKey = key
		h.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	case http.MethodDelete:
		if r.Header.Get("x-token") != h.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.mu.Lock()
		delete(h.store, key)
		h.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func TestCFKVRoundTrip(t *testing.T) {
	h := &kvHandler{token: "sekrit", store: map[string][]byte{}}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	tgt, err := New(cloudsync.TargetConfig{
		Name:           "cf",
		BaseURL:        ts.URL,
		Token:          "sekrit",
		FilenamePrefix: "sub/",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	name := "\u8ba2\u9605.txt" // 订阅.txt
	wantKey := "sub/" + name
	data := []byte("cfkv payload")
	url, err := cloudsync.Push(ctx, tgt, name, data)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if h.lastKey != wantKey {
		t.Errorf("server key = %q, want %q", h.lastKey, wantKey)
	}
	if !strings.HasPrefix(url, ts.URL+"/sub/") {
		t.Errorf("url = %q, want %s/sub/...", url, ts.URL)
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

func TestCFKVKeyTooLong(t *testing.T) {
	tgt, err := New(cloudsync.TargetConfig{
		Name:           "cf",
		BaseURL:        "http://127.0.0.1:1",
		Token:          "t",
		FilenamePrefix: "p/",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cloudsync.Push(context.Background(), tgt, strings.Repeat("x", 512), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "cfkv key too long") {
		t.Fatalf("err = %v, want cfkv key-too-long error", err)
	}
}

func TestRegisteredType(t *testing.T) {
	for _, want := range []string{Type} {
		found := false
		for _, rt := range cloudsync.RegisteredTypes() {
			if rt == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("type %q is not registered", want)
		}
	}
}
