package cloudsync

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"time"
)

// Policy controls retry behaviour. Zero values fall back to defaults.
type Policy struct {
	MaxRetries int           // retries after the first attempt; default 2 (3 attempts total)
	BaseDelay  time.Duration // default 400ms
	MaxDelay   time.Duration // default 4s
	Retriable  func(error) bool
	jitter     func() float64 // tests only
}

func (p Policy) withDefaults() Policy {
	if p.MaxRetries <= 0 {
		p.MaxRetries = 3
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = 400 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 4 * time.Second
	}
	if p.Retriable == nil {
		p.Retriable = IsRetryable
	}
	if p.jitter == nil {
		p.jitter = rand.Float64
	}
	return p
}

// IsRetryable reports whether err should be retried: HTTP 429/5xx, network
// timeouts/temporary errors, or any error implementing Temporary() bool.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var he *HTTPError
	if errors.As(err, &he) {
		return he.StatusCode == 429 || he.StatusCode >= 500
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	var tmp interface{ Temporary() bool }
	if errors.As(err, &tmp) && tmp.Temporary() {
		return true
	}
	return false
}

// Retry runs fn at least once, retrying retryable failures with exponential
// backoff plus jitter until ctx is done or MaxRetries is exhausted.
func Retry(ctx context.Context, policy Policy, fn func() error) error {
	p := policy.withDefaults()
	var err error
	for attempt := 0; attempt <= p.MaxRetries; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		// A done context always wins: the caller cancelled, don't mask that
		// with the operation's own error.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt >= p.MaxRetries || !p.Retriable(err) {
			return err
		}
		delay := p.BaseDelay * time.Duration(1<<min(attempt, 6))
		if delay > p.MaxDelay {
			delay = p.MaxDelay
		}
		// 80%–120% jitter to avoid thundering-herd retries.
		delay = time.Duration(float64(delay) * (0.8 + 0.4*p.jitter()))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}

// retryable wraps an error so IsRetryable treats it as temporary (used for
// eventual-consistency read-backs that return 404 or stale content).
type retryable struct{ err error }

func (e *retryable) Error() string { return e.err.Error() }
func (e *retryable) Unwrap() error { return e.err }
func (e *retryable) Temporary() bool {
	return true
}

// slash wraps err to make it obvious what operation failed.
func wrapop(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cloudsync: %s: %w", op, err)
}
