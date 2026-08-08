package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeTarget simulates a §7-protocol target for core-library tests.
type fakeTarget struct {
	name            string
	store           map[string][]byte
	verifyEnabled   bool
	failPushBefore  int // remaining synthetic push failures
	pushCount       int
	readUnsupported bool
	staleReads      int // return stale content this many times
	readErr         error
	readCount       int
}

func (f *fakeTarget) Name() string        { return f.name }
func (f *fakeTarget) VerifyEnabled() bool { return f.verifyEnabled }

func (f *fakeTarget) Push(ctx context.Context, filename string, data []byte) (string, error) {
	f.pushCount++
	if f.failPushBefore > 0 {
		f.failPushBefore--
		return "", &HTTPError{StatusCode: 500, Status: "Internal Server Error"}
	}
	f.store[filename] = append([]byte(nil), data...)
	return "https://example.com/" + filename, nil
}

func (f *fakeTarget) Read(ctx context.Context, filename string) ([]byte, error) {
	f.readCount++
	if f.readUnsupported {
		return nil, ErrReadUnsupported
	}
	if f.readErr != nil {
		return nil, f.readErr
	}
	if f.staleReads > 0 {
		f.staleReads--
		return []byte("stale"), nil
	}
	data, ok := f.store[filename]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeTarget) Delete(ctx context.Context, filename string) error {
	delete(f.store, filename)
	return nil
}

func newFakeTarget(name string) *fakeTarget {
	return &fakeTarget{name: name, store: map[string][]byte{}, verifyEnabled: true}
}

// init registers the fake provider so config parsing/validation can be tested
// without pulling real providers into the core package.
func init() {
	Register("test-provider", func(cfg TargetConfig) (Target, error) {
		return newFakeTarget(cfg.Name), nil
	})
}

// fastVerify makes read-back verification fast in tests.
func fastVerify(t *testing.T) {
	t.Helper()
	old := verifyRetryPolicy
	verifyRetryPolicy = Policy{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	t.Cleanup(func() { verifyRetryPolicy = old })
}

func TestPushOK(t *testing.T) {
	fastVerify(t)
	ctx := context.Background()
	tgt := newFakeTarget("t")
	url, err := Push(ctx, tgt, "sub.txt", []byte("hello"))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if url != "https://example.com/sub.txt" {
		t.Errorf("url = %q", url)
	}
	if got := tgt.store["sub.txt"]; string(got) != "hello" {
		t.Errorf("store = %q", got)
	}
}

func TestPushRetriesTransientFailure(t *testing.T) {
	tgt := newFakeTarget("t")
	tgt.failPushBefore = 2
	url, err := Push(context.Background(), tgt, "a.txt", []byte("x"))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if url == "" {
		t.Error("want non-empty url")
	}
	if tgt.pushCount != 3 {
		t.Errorf("pushCount = %d, want 3", tgt.pushCount)
	}
}

func TestPushVerificationEventualConsistency(t *testing.T) {
	fastVerify(t)
	tgt := newFakeTarget("t")
	tgt.staleReads = 2 // stale twice, then correct
	_, err := Push(context.Background(), tgt, "a.txt", []byte("payload"))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestPushVerificationFailsOnPersistentMismatch(t *testing.T) {
	fastVerify(t)
	tgt := newFakeTarget("t")
	tgt.staleReads = 100
	url, err := Push(context.Background(), tgt, "a.txt", []byte("payload"))
	if err == nil {
		t.Fatal("want verification error")
	}
	if !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("err = %v", err)
	}
	if url != "https://example.com/a.txt" {
		t.Errorf("url should still be returned, got %q", url)
	}
}

func TestPushSkipsVerificationWhenUnsupported(t *testing.T) {
	tgt := newFakeTarget("t")
	tgt.readUnsupported = true
	_, err := Push(context.Background(), tgt, "a.txt", []byte("payload"))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestPushVerifyDisabled(t *testing.T) {
	tgt := newFakeTarget("t")
	tgt.verifyEnabled = false
	tgt.readErr = errors.New("should not be called")
	_, err := Push(context.Background(), tgt, "a.txt", []byte("payload"))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestPushTooLarge(t *testing.T) {
	tgt := newFakeTarget("t")
	data := bytes.Repeat([]byte("x"), MaxPayloadBytes+1)
	_, err := Push(context.Background(), tgt, "big.bin", data)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
}

func TestPushRejectsTraversal(t *testing.T) {
	tgt := newFakeTarget("t")
	_, err := Push(context.Background(), tgt, "../evil.txt", []byte("x"))
	if err == nil {
		t.Fatal("want error for traversal")
	}
	if tgt.pushCount != 0 {
		t.Errorf("pushCount = %d, want 0", tgt.pushCount)
	}
}

func TestBroadcastPartialFailure(t *testing.T) {
	fastVerify(t)
	good := newFakeTarget("good")
	bad := newFakeTarget("bad")
	bad.failPushBefore = 99
	results := Broadcast(context.Background(), []Target{good, bad}, "a.txt", []byte("x"))
	if len(results) != 2 {
		t.Fatalf("len = %d", len(results))
	}
	if results[0].URL == "" || results[0].Err != nil {
		t.Errorf("good result = %+v", results[0])
	}
	if results[1].Err == nil {
		t.Errorf("bad result should have error: %+v", results[1])
	}
	if got := BroadcastFailures(results); got == "" {
		t.Error("want failure summary")
	}
}

func TestRedactor(t *testing.T) {
	r := NewRedactor("sekrit")
	got := r.Redact("url with sekrit inside")
	if strings.Contains(got, "sekrit") || !strings.Contains(got, "***") {
		t.Errorf("Redact = %q", got)
	}
}
