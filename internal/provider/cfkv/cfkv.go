// Package cfkv provides the Cloudflare KV target ("cfkv").
// The companion worker lives in server/cf-worker and speaks the §7 protocol.
package cfkv

import (
	"fmt"

	"cloudsync/internal/cloudsync"
	"cloudsync/internal/provider/httpprov"
)

const Type = "cfkv"

func init() {
	cloudsync.Register(Type, New)
}

// New builds a cfkv target. Cloudflare KV keys accept arbitrary UTF-8
// (including "/" for prefixes), so the logical filename is kept as-is after
// core sanitization; only the 512-byte key limit is enforced here.
func New(cfg cloudsync.TargetConfig) (cloudsync.Target, error) {
	verify := true
	if cfg.Verify != nil {
		verify = *cfg.Verify
	}
	retries, err := cfg.IntOption("retries", 3)
	if err != nil {
		return nil, err
	}
	timeout, err := cfg.DurationOption("timeout", 0)
	if err != nil {
		return nil, err
	}
	prefix := cfg.FilenamePrefix
	return httpprov.New(httpprov.Config{
		Name:       cfg.Name,
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Verify:     verify,
		MaxRetries: retries,
		Timeout:    timeout,
		KeyFor: func(filename string) (string, error) {
			key := cloudsync.JoinKey(prefix, filename)
			if len(key) > cloudsync.MaxKeyBytes {
				return "", fmt.Errorf("cloudsync: cfkv key too long (%d bytes, max %d)", len(key), cloudsync.MaxKeyBytes)
			}
			return key, nil
		},
	})
}
