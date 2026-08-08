// Package edgeone provides Tencent EdgeOne targets:
//
//	edgeone-kv   - EdgeOne KV, keys restricted to [A-Za-z0-9_] (safe-key encoding)
//	edgeone-blob - EdgeOne Blob, keeps directory/Unicode names (strong consistency)
//
// Companion edge functions live in server/edgeone-kv and server/edgeone-blob.
package edgeone

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"cloudsync/internal/cloudsync"
	"cloudsync/internal/provider/httpprov"
)

const (
	// TypeKV is the EdgeOne KV target type.
	TypeKV = "edgeone-kv"
	// TypeBlob is the EdgeOne Blob target type.
	TypeBlob = "edgeone-blob"
)

func init() {
	cloudsync.Register(TypeKV, newKV)
	cloudsync.Register(TypeBlob, newBlob)
}

func newKV(cfg cloudsync.TargetConfig) (cloudsync.Target, error) {
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
			return safeKey(cloudsync.JoinKey(prefix, filename))
		},
	})
}

func newBlob(cfg cloudsync.TargetConfig) (cloudsync.Target, error) {
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
	bucket, prefix := cfg.Bucket, cfg.FilenamePrefix
	return httpprov.New(httpprov.Config{
		Name:       cfg.Name,
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Verify:     verify,
		MaxRetries: retries,
		Timeout:    timeout,
		KeyFor: func(filename string) (string, error) {
			key := cloudsync.JoinKey(bucket, prefix, filename)
			if len(key) > cloudsync.MaxKeyBytes {
				return "", fmt.Errorf("cloudsync: edgeone-blob key too long (%d bytes, max %d)", len(key), cloudsync.MaxKeyBytes)
			}
			return key, nil
		},
	})
}

// safeKey converts an arbitrary key into the EdgeOne KV charset
// [A-Za-z0-9_] with a maximum length of 512 bytes:
//
//   - already safe keys are returned unchanged;
//   - otherwise every unsafe byte becomes "_xx" (hex);
//   - if that still exceeds 512 bytes, a stable short hash is used.
func safeKey(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("cloudsync: edgeone-kv key is empty")
	}
	if len(name) <= cloudsync.MaxKeyBytes && isSafe(name) {
		return name, nil
	}
	var b []byte
	b = append(b, 's', '_')
	for i := 0; i < len(name); i++ {
		ch := name[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_':
			b = append(b, ch)
		default:
			b = append(b, '_')
			b = append(b, hexDigit(ch>>4), hexDigit(ch&0xf))
		}
	}
	if len(b) <= cloudsync.MaxKeyBytes {
		return string(b), nil
	}
	h := sha256.Sum256([]byte(name))
	return "s_" + hex.EncodeToString(h[:])[:64], nil
}

func hexDigit(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'a' + n - 10
}

func isSafe(s string) bool {
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_':
		default:
			return false
		}
	}
	return true
}
