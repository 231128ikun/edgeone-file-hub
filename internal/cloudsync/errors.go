package cloudsync

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors shared by the core library and HTTP providers.
var (
	// ErrNotFound is returned by Read/Delete when the remote object does not exist.
	ErrNotFound = errors.New("cloudsync: object not found")
	// ErrUnauthorized is returned when the write token is missing or rejected (HTTP 401/403).
	ErrUnauthorized = errors.New("cloudsync: forbidden: missing or invalid write token")
	// ErrReadUnsupported is returned by targets that cannot read back.
	ErrReadUnsupported = errors.New("cloudsync: read is not supported by this target")
	// ErrDeleteUnsupported is returned by targets that cannot delete.
	ErrDeleteUnsupported = errors.New("cloudsync: delete is not supported by this target")
	// ErrPayloadTooLarge is returned when data exceeds the 25 MiB platform limit.
	ErrPayloadTooLarge = errors.New("cloudsync: payload exceeds 25 MiB platform limit")
	// ErrTokenRequired is returned when a write/delete is attempted without a token.
	ErrTokenRequired = errors.New("cloudsync: target requires a write token (set token or ${ENV_VAR})")
)

// HTTPError carries an unexpected non-2xx HTTP status from a provider.
// Status codes 429 and >=500 are treated as retryable by IsRetryable.
type HTTPError struct {
	StatusCode int
	Status     string // e.g. "502 Bad Gateway"
	Body       string
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("cloudsync: http %d", e.StatusCode)
	if e.Status != "" {
		msg += " " + e.Status
	}
	if body := strings.TrimSpace(e.Body); body != "" {
		if len(body) > 160 {
			body = body[:160] + "…"
		}
		msg += ": " + body
	}
	return msg
}
