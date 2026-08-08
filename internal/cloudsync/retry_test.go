package cloudsync

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryRetriesHTTP500(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), Policy{MaxRetries: 3, BaseDelay: time.Millisecond, jitter: fixedJitter(0.5)}, func() error {
		attempts++
		return &HTTPError{StatusCode: 500, Status: "Internal Server Error"}
	})
	if err == nil {
		t.Fatal("want error")
	}
	if attempts != 4 {
		t.Errorf("want 4 attempts, got %d", attempts)
	}
}

func TestRetryStopsOnHTTP400(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), Policy{MaxRetries: 3, BaseDelay: time.Millisecond, jitter: fixedJitter(0.5)}, func() error {
		attempts++
		return &HTTPError{StatusCode: 400, Status: "Bad Request", Body: "nope"}
	})
	if err == nil {
		t.Fatal("want error")
	}
	if attempts != 1 {
		t.Errorf("want 1 attempt, got %d", attempts)
	}
}

func TestRetryStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Retry(ctx, Policy{MaxRetries: 3, BaseDelay: time.Millisecond}, func() error {
		return errors.New("boom")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestRetryRetriesTemporary(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), Policy{MaxRetries: 2, BaseDelay: time.Millisecond, jitter: fixedJitter(0.5)}, func() error {
		attempts++
		return &MismatchError{Name: "x", Want: 10, Got: 5}
	})
	if err == nil {
		t.Fatal("want error")
	}
	if attempts != 3 {
		t.Errorf("want 3 attempts, got %d", attempts)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{&HTTPError{StatusCode: 429}, true},
		{&HTTPError{StatusCode: 500}, true},
		{&HTTPError{StatusCode: 503}, true},
		{&HTTPError{StatusCode: 400}, false},
		{&HTTPError{StatusCode: 404}, false},
		{ErrNotFound, false},
		{&MismatchError{}, true},
		{errors.New("plain"), false},
	}
	for _, tt := range tests {
		if got := IsRetryable(tt.err); got != tt.want {
			t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestHTTPError(t *testing.T) {
	e := &HTTPError{StatusCode: 502, Status: "Bad Gateway", Body: "upstream failed"}
	if got := e.Error(); got == "" || !errors.Is(e, e) {
		t.Errorf("HTTPError.Error() = %q", got)
	}
}

func fixedJitter(v float64) func() float64 { return func() float64 { return v } }
