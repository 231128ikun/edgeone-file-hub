package cloudsync

import (
	"fmt"
	"path"
	"strings"
)

const (
	// MaxPayloadBytes is the platform value-size limit (CF KV, EdgeOne KV
	// and EdgeOne Blob all cap at 25 MiB per object).
	MaxPayloadBytes = 25 << 20 // 25 MiB
	// MaxKeyBytes is the remote key length limit (EdgeOne KV: <=512 bytes;
	// CF KV shares the same limit; sane for Blob too).
	MaxKeyBytes = 512
)

// SanitizeFilename normalizes a remote object name:
//   - uses forward slashes (allows directory-like names for Blob),
//   - rejects path traversal, absolute paths, control characters,
//   - rejects names longer than MaxKeyBytes.
//
// Unicode is preserved as-is; providers that need a restricted charset
// (EdgeOne KV) apply their own SafeKey encoding afterwards.
func SanitizeFilename(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("cloudsync: filename is empty")
	}
	clean := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" {
		return "", fmt.Errorf("cloudsync: invalid filename %q", name)
	}
	// Reject traversal before path.Clean can silently normalize it away.
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return "", fmt.Errorf("cloudsync: path traversal is not allowed in filename %q", name)
		}
	}
	clean = path.Clean(clean)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("cloudsync: invalid filename %q", name)
	}
	for _, r := range clean {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("cloudsync: control character in filename %q", name)
		}
	}
	if len(clean) > MaxKeyBytes {
		return "", fmt.Errorf("cloudsync: filename too long (%d bytes, max %d)", len(clean), MaxKeyBytes)
	}
	return clean, nil
}

// JoinKey joins path parts with "/" (leading/trailing slashes trimmed).
// Empty parts are skipped.
func JoinKey(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('/')
		}
		b.WriteString(p)
	}
	return b.String()
}
