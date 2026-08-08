package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
)

// Push uploads data to target under filename and returns the public URL.
// Read-back verification is enabled by default (see PushOptions).
func Push(ctx context.Context, t Target, filename string, data []byte) (string, error) {
	return PushWithOptions(ctx, t, filename, data, PushOptions{})
}

// PushOptions overrides default push behaviour.
type PushOptions struct {
	// Verify overrides the target's verify setting; nil keeps the target default.
	Verify *bool
	// MaxRetries overrides the retry count for the upload (0 = default).
	MaxRetries int
}

// verifyRetryPolicy controls read-back verification retries. It is a
// package-level variable so tests can shorten the backoff.
var verifyRetryPolicy = Policy{
	MaxRetries: 4,
	BaseDelay:  500 * time.Millisecond,
	MaxDelay:   2 * time.Second,
}

// PushWithOptions uploads data with optional read-back verification.
func PushWithOptions(ctx context.Context, t Target, filename string, data []byte, opts PushOptions) (string, error) {
	name, err := SanitizeFilename(filename)
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", errors.New("cloudsync: nil target")
	}
	if len(data) > MaxPayloadBytes {
		return "", fmt.Errorf("%w: %d bytes", ErrPayloadTooLarge, len(data))
	}

	verify := true
	if opts.Verify != nil {
		verify = *opts.Verify
	} else if v, ok := t.(interface{ VerifyEnabled() bool }); ok {
		verify = v.VerifyEnabled()
	}

	policy := Policy{MaxRetries: opts.MaxRetries}
	var url string
	err = Retry(ctx, policy, func() error {
		u, perr := t.Push(ctx, name, data)
		if perr != nil {
			return perr
		}
		url = u
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("cloudsync: push %q: %w", name, err)
	}

	if verify {
		if err := verifyReadBack(ctx, t, name, data); err != nil {
			return url, fmt.Errorf("cloudsync: push %q succeeded but verification failed: %w", name, err)
		}
	}
	return url, nil
}

// verifyReadBack reads the object back, tolerating eventual consistency
// (the storage may return a stale value or 404 immediately after the write).
func verifyReadBack(ctx context.Context, t Target, name string, want []byte) error {
	err := Retry(ctx, verifyRetryPolicy, func() error {
		got, rerr := t.Read(ctx, name)
		if rerr != nil {
			if errors.Is(rerr, ErrReadUnsupported) {
				return errVerifySkipped
			}
			if errors.Is(rerr, ErrNotFound) {
				// Nothing there yet: likely eventual consistency, retry.
				return &retryable{err: rerr}
			}
			return rerr
		}
		if !bytes.Equal(got, want) {
			return &MismatchError{Name: name, Want: len(want), Got: len(got)}
		}
		return nil
	})
	if errors.Is(err, errVerifySkipped) {
		return nil // provider cannot read back; nothing to verify
	}
	var mm *MismatchError
	if errors.As(err, &mm) {
		return fmt.Errorf("content mismatch (%d bytes expected, %d read back); the store may still be converging", mm.Want, mm.Got)
	}
	return err
}

var errVerifySkipped = errors.New("cloudsync: verification skipped (target cannot read back)")

// MismatchError reports a read-back verification failure.
type MismatchError struct {
	Name string
	Want int
	Got  int
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("%q: expected %d bytes, read back %d", e.Name, e.Want, e.Got)
}

func (e *MismatchError) Temporary() bool {
	return true
}
