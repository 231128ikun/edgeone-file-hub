package cloudsync

import (
	"context"
	"fmt"
	"strings"
)

// BroadcastResult is the outcome of pushing to a single target.
type BroadcastResult struct {
	Target string
	URL    string
	Err    error
}

// Broadcast pushes data to every target (sequential, keeps going on failure)
// and returns one result per target. Push verification still applies.
func Broadcast(ctx context.Context, targets []Target, filename string, data []byte) []BroadcastResult {
	results := make([]BroadcastResult, 0, len(targets))
	for _, t := range targets {
		res := BroadcastResult{Target: t.Name()}
		url, err := Push(ctx, t, filename, data)
		if err != nil {
			res.Err = err
		} else {
			res.URL = url
		}
		results = append(results, res)
	}
	return results
}

// BroadcastFailures returns a human-readable summary of failed targets.
func BroadcastFailures(results []BroadcastResult) string {
	var failed []string
	for _, r := range results {
		if r.Err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", r.Target, r.Err))
		}
	}
	if len(failed) == 0 {
		return ""
	}
	return fmt.Sprintf("cloudsync: %d target(s) failed: %s", len(failed), strings.Join(failed, "; "))
}
