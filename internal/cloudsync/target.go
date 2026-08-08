// Package cloudsync is the core of the cloudsync tool: a small pluggable
// library that pushes text/files to public cloud storage and returns a
// stable public URL.
//
// The only hard dependency is gopkg.in/yaml.v3; providers are registered
// through Register() by their own packages (see internal/provider).
package cloudsync

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Target describes a pushable cloud target (one configured "destination").
type Target interface {
	// Name returns the configured target name.
	Name() string
	// Push uploads data under filename and returns a public URL. Filename is
	// the remote object name (already sanitized by Push; providers may still
	// transform it for platform key constraints).
	Push(ctx context.Context, filename string, data []byte) (string, error)
	// Read fetches the current remote content. Providers that cannot read
	// may return ErrReadUnsupported.
	Read(ctx context.Context, filename string) ([]byte, error)
	// Delete removes the remote object. It may return ErrDeleteUnsupported.
	Delete(ctx context.Context, filename string) error
}

// Factory builds a Target from a validated TargetConfig.
type Factory func(cfg TargetConfig) (Target, error)

var registry = map[string]Factory{}

// Register adds a provider factory under a type name (e.g. "cfkv").
// It panics on duplicate registration, which is a programmer error.
func Register(kind string, f Factory) {
	if kind == "" {
		panic("cloudsync: Register with empty type name")
	}
	if f == nil {
		panic("cloudsync: nil factory for " + kind)
	}
	if _, dup := registry[kind]; dup {
		panic("cloudsync: duplicate provider registration: " + kind)
	}
	registry[kind] = f
}

// NewTarget builds a Target for cfg after validating it.
func NewTarget(cfg TargetConfig) (Target, error) {
	if cfg.Name == "" {
		cfg.Name = cfg.Type
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	f, ok := registry[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("cloudsync: unknown target type %q (registered: %s)", cfg.Type, strings.Join(RegisteredTypes(), ", "))
	}
	return f(cfg)
}

// RegisteredTypes returns the sorted provider type names.
func RegisteredTypes() []string {
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
