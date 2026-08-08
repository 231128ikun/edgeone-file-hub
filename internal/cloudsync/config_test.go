package cloudsync

import (
	"strings"
	"testing"
)

const yamlDoc = `
targets:
  cf:
    type: test-provider
    base_url: https://example.com
    token: ${CLOUDSYNC_TEST_TOKEN}
    filename_prefix: sub/
    retries: 5
  daily:
    type: test-provider
    base_url: https://example.com/daily
    bucket: subs
    timeout: 30
`

func TestParseConfigYAML(t *testing.T) {
	t.Setenv("CLOUDSYNC_TEST_TOKEN", "secret-123")
	t.Setenv("CLOUDSYNC_EMPTY", "")
	cfg, err := ParseConfig([]byte(yamlDoc), ".yaml")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cf := cfg.Targets["cf"]
	if cf.Token != "secret-123" {
		t.Errorf("token = %q, want expanded env", cf.Token)
	}
	if cf.FilenamePrefix != "sub/" {
		t.Errorf("filename_prefix = %q, want sub/", cf.FilenamePrefix)
	}
	if cf.Options["retries"] != "5" {
		t.Errorf("options.retries = %q", cf.Options["retries"])
	}
	if got := cf.Options["bucket"]; got != "" {
		t.Errorf("bucket should be a known field, got option %q", got)
	}
	// daily target options from inline scalar (timeout: 30 → "30")
	if cfg.Targets["daily"].Options["timeout"] != "30" {
		t.Errorf("daily timeout option = %q", cfg.Targets["daily"].Options["timeout"])
	}
}

func TestParseConfigJSON(t *testing.T) {
	t.Setenv("CLOUDSYNC_TEST_TOKEN", "json-token")
	data := `{"targets":{"cf":{"type":"test-provider","base_url":"https://example.com","token":"${CLOUDSYNC_TEST_TOKEN}","timeout":30,"note":"x"}}}`
	cfg, err := ParseConfig([]byte(data), ".json")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cf := cfg.Targets["cf"]
	if cf.Token != "json-token" {
		t.Errorf("token = %q", cf.Token)
	}
	if cf.Options["timeout"] != "30" {
		t.Errorf("timeout option = %q", cf.Options["timeout"])
	}
	if cf.Options["note"] != "x" {
		t.Errorf("note option = %q", cf.Options["note"])
	}
}

func TestExpandEnvDefault(t *testing.T) {
	// unset variable with default
	if got := ExpandEnv("${NO_SUCH_VAR_12345:-fallback}"); got != "fallback" {
		t.Errorf("default = %q", got)
	}
	t.Setenv("CLOUDSYNC_SET_VAR", "value")
	if got := ExpandEnv("${CLOUDSYNC_SET_VAR:-fallback}"); got != "value" {
		t.Errorf("env = %q", got)
	}
	if got := ExpandEnv("no-refs"); got != "no-refs" {
		t.Errorf("plain = %q", got)
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		msg  string
	}{
		{"no targets", "targets: {}", "no targets"},
		{"unknown type", "targets:\n  a:\n    type: nope\n    base_url: https://x.com", "unknown provider type"},
		{"missing type", "targets:\n  a:\n    base_url: https://x.com", "type is required"},
		{"missing base_url", "targets:\n  a:\n    type: test-provider", "base_url is required"},
		{"bad scheme", "targets:\n  a:\n    type: test-provider\n    base_url: ftp://x.com", "must be http(s)"},
		{"traversal prefix", "targets:\n  a:\n    type: test-provider\n    base_url: https://x.com\n    filename_prefix: ../up", "path traversal"},
		{"bad bucket", "targets:\n  a:\n    type: test-provider\n    base_url: https://x.com\n    bucket: ../up", "invalid bucket"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tt.doc), ".yaml")
			if err == nil || !strings.Contains(err.Error(), tt.msg) {
				t.Errorf("want error containing %q, got %v", tt.msg, err)
			}
		})
	}
}

func TestBuildSetsNames(t *testing.T) {
	t.Setenv("CLOUDSYNC_TEST_TOKEN", "x")
	cfg, err := ParseConfig([]byte(yamlDoc), ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	targets, err := cfg.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if targets["cf"].Name() != "cf" {
		t.Errorf("Name() = %q", targets["cf"].Name())
	}
	if targets["daily"].Name() != "daily" {
		t.Errorf("Name() = %q", targets["daily"].Name())
	}
}

func TestDefaultConfigPathsIncludesEnv(t *testing.T) {
	t.Setenv("CLOUDSYNC_CONFIG", "C:/tmp/mycloud.yaml")
	paths := DefaultConfigPaths()
	if paths[0] != "C:/tmp/mycloud.yaml" {
		t.Errorf("first path = %q", paths[0])
	}
}
