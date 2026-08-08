package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// cliKV is a minimal in-memory implementation of the section-7 protocol.
type cliKV struct {
	mu      sync.Mutex
	token   string
	store   map[string][]byte
	lastKey string
}

func newCLIKV(token string) *cliKV {
	return &cliKV{token: token, store: map[string][]byte{}}
}

func (s *cliKV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		s.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"url":"http://%s%s","size":%d}`, r.Host, r.URL.Path, len(body))
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

func writeConfigBody(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cloudsync.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func twoTargetConfig(baseURL string) string {
	return twoTargetConfigWithToken(baseURL, "sekrit")
}

func twoTargetConfigWithToken(baseURL, token string) string {
	return fmt.Sprintf(`targets:
  cf:
    type: cfkv
    base_url: %s
    token: %s
    verify: false
  edge:
    type: edgeone-blob
    base_url: %s
    token: %s
    verify: false
`, baseURL, token, baseURL, token)
}

func TestRunVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, &out, &errb); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "cloudsync 0.1.0") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestRunHelp(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"help"}, &out, &errb); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errb.String())
	}
	for _, want := range []string{"push", "read", "delete", "list", "config-check", "version"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help stdout missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"frobnicate"}, &out, &errb); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestRunNoConfigFile(t *testing.T) {
	var out, errb bytes.Buffer
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	if code := run([]string{"push", "cf", "a.txt", "-config", missing}, &out, &errb); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "cloudsync:") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestRunPushReadDelete(t *testing.T) {
	srv := newCLIKV("sekrit")
	ts := httptest.NewServer(srv)
	defer ts.Close()
	cfg := writeConfigBody(t, twoTargetConfig(ts.URL))

	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("hello cli\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := run([]string{"push", "cf", file, "-config", cfg}, &out, &errb); code != 0 {
		t.Fatalf("push code = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), ts.URL) || !strings.Contains(out.String(), "/a.txt") {
		t.Errorf("push stdout = %q", out.String())
	}
	if srv.lastKey != "a.txt" {
		t.Errorf("lastKey = %q, want a.txt", srv.lastKey)
	}

	out.Reset()
	errb.Reset()
	if code := run([]string{"read", "cf", "a.txt", "-config", cfg}, &out, &errb); code != 0 {
		t.Fatalf("read code = %d, stderr = %s", code, errb.String())
	}
	if out.String() != "hello cli\n" {
		t.Errorf("read stdout = %q", out.String())
	}

	out.Reset()
	errb.Reset()
	if code := run([]string{"delete", "cf", "a.txt", "-config", cfg}, &out, &errb); code != 0 {
		t.Fatalf("delete code = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "deleted a.txt from cf") {
		t.Errorf("delete stdout = %q", out.String())
	}

	out.Reset()
	errb.Reset()
	if code := run([]string{"read", "cf", "a.txt", "-config", cfg}, &out, &errb); code != 1 {
		t.Fatalf("read-after-delete code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "not found") {
		t.Errorf("read-after-delete stderr = %q", errb.String())
	}
}

func TestRunPushCustomUnicodeName(t *testing.T) {
	srv := newCLIKV("sekrit")
	ts := httptest.NewServer(srv)
	defer ts.Close()
	cfg := writeConfigBody(t, twoTargetConfig(ts.URL))

	file := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	remote := "sub/\u8ba2\u9605 1.txt" // sub/订阅 1.txt

	var out, errb bytes.Buffer
	if code := run([]string{"push", "cf", file, "--name", remote, "-config", cfg}, &out, &errb); code != 0 {
		t.Fatalf("push code = %d, stderr = %s", code, errb.String())
	}
	if srv.lastKey != remote {
		t.Errorf("lastKey = %q, want %q", srv.lastKey, remote)
	}

	out.Reset()
	errb.Reset()
	if code := run([]string{"read", "cf", remote, "-config", cfg}, &out, &errb); code != 0 {
		t.Fatalf("read code = %d, stderr = %s", code, errb.String())
	}
	if out.String() != "content\n" {
		t.Errorf("read stdout = %q", out.String())
	}
}

func TestRunPushAll(t *testing.T) {
	srv := newCLIKV("sekrit")
	ts := httptest.NewServer(srv)
	defer ts.Close()
	cfg := writeConfigBody(t, twoTargetConfig(ts.URL))

	file := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := run([]string{"push", "--all", file, "-config", cfg}, &out, &errb); code != 0 {
		t.Fatalf("push --all code = %d, stderr = %s", code, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("push --all stdout = %q, want 2 lines", out.String())
	}
	for _, line := range lines {
		if !strings.Contains(line, "\t") || !strings.Contains(line, ts.URL) {
			t.Errorf("push --all line = %q", line)
		}
	}
	if srv.lastKey != "x.txt" {
		t.Errorf("lastKey = %q, want x.txt", srv.lastKey)
	}
}

func TestRunListMasksTokens(t *testing.T) {
	cfg := writeConfigBody(t, fmt.Sprintf(`targets:
  cf:
    type: cfkv
    base_url: %s
    token: sekrit
    verify: false
  edge:
    type: edgeone-blob
    base_url: %s
    token: ${CLOUDSYNC_TEST_TOKEN:-fallback}
    verify: false
`, "https://example.com", "https://example.com"))
	var out, errb bytes.Buffer
	if code := run([]string{"list", "-config", cfg}, &out, &errb); code != 0 {
		t.Fatalf("list code = %d, stderr = %s", code, errb.String())
	}
	s := out.String()
	for _, want := range []string{"NAME", "cf", "edge", "cfkv", "edgeone-blob", "https://example.com", "***"} {
		if !strings.Contains(s, want) {
			t.Errorf("list stdout missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "sekrit") || strings.Contains(s, "fallback") {
		t.Errorf("list stdout leaked a token:\n%s", s)
	}
}

func TestRunConfigCheck(t *testing.T) {
	cfg := writeConfigBody(t, fmt.Sprintf(`targets:
  cf:
    type: cfkv
    base_url: %s
    token: sekrit
    verify: false
  edge:
    type: edgeone-blob
    base_url: %s
    token: ${CLOUDSYNC_TEST_TOKEN:-fallback}
    verify: false
`, "https://example.com", "https://example.com"))
	var out, errb bytes.Buffer
	if code := run([]string{"config-check", "-config", cfg}, &out, &errb); code != 0 {
		t.Fatalf("config-check code = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "config OK") || !strings.Contains(out.String(), "2 targets") {
		t.Errorf("config-check stdout = %q", out.String())
	}
}

func TestRunUsageErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"push"}, &out, &errb); code != 2 {
		t.Fatalf("push without args code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "usage:") {
		t.Errorf("stderr = %q", errb.String())
	}
}
