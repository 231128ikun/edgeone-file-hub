package cloudsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration file: named targets.
//
//	config:
//	  targets:
//	    cf:
//	      type: cfkv
//	      base_url: https://example.pages.dev
//	      token: ${CLOUDSYNC_CF_TOKEN}
//	      filename_prefix: sub/
type Config struct {
	Targets map[string]TargetConfig `yaml:"targets" json:"targets"`
}

// TargetConfig describes one pushable target. Provider-specific options
// (e.g. timeout, retries) live in Options.
type TargetConfig struct {
	// Name is filled in by the loader/CLI, never read from the file.
	Name string `yaml:"-" json:"-"`
	// Type selects the provider: cfkv, edgeone-kv or edgeone-blob.
	Type string `yaml:"type" json:"type"`
	// BaseURL is the origin of the deployed edge function (§7 protocol).
	BaseURL string `yaml:"base_url" json:"base_url"`
	// Token may be a literal or ${ENV_VAR}; expanded at load time.
	Token string `yaml:"token" json:"token,omitempty"`
	// FilenamePrefix is prepended to every remote key (e.g. "sub/").
	FilenamePrefix string `yaml:"filename_prefix" json:"filename_prefix,omitempty"`
	// Bucket is a directory prefix used by edgeone-blob (e.g. "subs").
	Bucket string `yaml:"bucket" json:"bucket,omitempty"`
	// Verify enables read-back verification after push (default true).
	Verify *bool `yaml:"verify" json:"verify,omitempty"`
	// Options carries provider-specific settings such as timeout / retries.
	Options map[string]string `yaml:",inline" json:"options,omitempty"`
}

// knownFields is used by the custom YAML/JSON decoders to separate
// provider-specific options from the built-in fields.
var knownFields = map[string]bool{
	"type": true, "base_url": true, "token": true, "filename_prefix": true,
	"bucket": true, "verify": true, "options": true,
}

// UnmarshalYAML decodes known fields explicitly and collects every other key
// into Options (values are converted to strings).
func (tc *TargetConfig) UnmarshalYAML(node *yaml.Node) error {
	var plain struct {
		Type           string `yaml:"type"`
		BaseURL        string `yaml:"base_url"`
		Token          string `yaml:"token"`
		FilenamePrefix string `yaml:"filename_prefix"`
		Bucket         string `yaml:"bucket"`
		Verify         *bool  `yaml:"verify"`
	}
	if err := node.Decode(&plain); err != nil {
		return err
	}
	raw := map[string]any{}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	tc.Type, tc.BaseURL = plain.Type, plain.BaseURL
	tc.Token, tc.FilenamePrefix = plain.Token, plain.FilenamePrefix
	tc.Bucket, tc.Verify = plain.Bucket, plain.Verify
	tc.Options = stringMap(raw)
	return nil
}

// UnmarshalJSON mirrors UnmarshalYAML for .json config files.
func (tc *TargetConfig) UnmarshalJSON(data []byte) error {
	var plain struct {
		Type           string `json:"type"`
		BaseURL        string `json:"base_url"`
		Token          string `json:"token"`
		FilenamePrefix string `json:"filename_prefix"`
		Bucket         string `json:"bucket"`
		Verify         *bool  `json:"verify"`
	}
	if err := json.Unmarshal(data, &plain); err != nil {
		return err
	}
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	tc.Type, tc.BaseURL = plain.Type, plain.BaseURL
	tc.Token, tc.FilenamePrefix = plain.Token, plain.FilenamePrefix
	tc.Bucket, tc.Verify = plain.Bucket, plain.Verify
	tc.Options = stringMap(raw)
	return nil
}

func stringMap(raw map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range raw {
		if knownFields[k] {
			continue
		}
		switch vv := v.(type) {
		case string:
			out[k] = vv
		case nil:
			out[k] = ""
		default:
			out[k] = fmt.Sprint(vv)
		}
	}
	return out
}

// LoadConfig reads a YAML or JSON config file, expands environment
// variables and validates it.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cloudsync: read config %s: %w", path, err)
	}
	cfg, err := ParseConfig(data, filepath.Ext(path))
	if err != nil {
		return nil, fmt.Errorf("cloudsync: config %s: %w", path, err)
	}
	return cfg, nil
}

// ParseConfig parses config data. ext selects the format (".json" or YAML).
func ParseConfig(data []byte, ext string) (*Config, error) {
	var cfg Config
	var err error
	if strings.EqualFold(ext, ".json") {
		err = json.Unmarshal(data, &cfg)
	} else {
		err = yaml.Unmarshal(data, &cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	cfg.expandEnv()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) expandEnv() {
	for name, tc := range c.Targets {
		tc.BaseURL = ExpandEnv(tc.BaseURL)
		tc.Token = ExpandEnv(tc.Token)
		tc.FilenamePrefix = ExpandEnv(tc.FilenamePrefix)
		tc.Bucket = ExpandEnv(tc.Bucket)
		for k, v := range tc.Options {
			tc.Options[k] = ExpandEnv(v)
		}
		c.Targets[name] = tc
	}
}

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// ExpandEnv replaces ${VAR} and ${VAR:-default} references in s with the
// environment value. Unset variables become "" (or the default).
func ExpandEnv(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return envPattern.ReplaceAllStringFunc(s, func(m string) string {
		parts := envPattern.FindStringSubmatch(m)
		key, hasDefault := parts[1], parts[2] != ""
		if v, ok := os.LookupEnv(key); ok {
			if v == "" && hasDefault {
				return parts[3]
			}
			return v
		}
		if hasDefault {
			return parts[3]
		}
		return ""
	})
}

// Validate checks the configuration: every target has a registered type,
// an http(s) base_url, and a sane filename_prefix.
func (c *Config) Validate() error {
	if len(c.Targets) == 0 {
		return errors.New("cloudsync: config has no targets")
	}
	names := make([]string, 0, len(c.Targets))
	for name := range c.Targets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tc := c.Targets[name]
		tc.Name = name
		if err := tc.Validate(); err != nil {
			return fmt.Errorf("cloudsync: target %q: %w", name, err)
		}
	}
	return nil
}

// Validate checks a single target config.
func (tc *TargetConfig) Validate() error {
	if tc.Type == "" {
		return errors.New("type is required")
	}
	if _, ok := registry[tc.Type]; !ok {
		return fmt.Errorf("unknown provider type %q (registered: %s)", tc.Type, strings.Join(RegisteredTypes(), ", "))
	}
	if tc.BaseURL == "" {
		return errors.New("base_url is required")
	}
	u, err := url.Parse(tc.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base_url must be http(s), got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("base_url is missing a host")
	}
	if tc.FilenamePrefix != "" {
		if _, err := SanitizeFilename(strings.TrimSuffix(tc.FilenamePrefix, "/")); err != nil {
			return fmt.Errorf("invalid filename_prefix: %w", err)
		}
	}
	if tc.Bucket != "" {
		if _, err := SanitizeFilename(tc.Bucket); err != nil {
			return fmt.Errorf("invalid bucket: %w", err)
		}
	}
	return nil
}

// Build constructs every configured target.
func (c *Config) Build() (map[string]Target, error) {
	targets := make(map[string]Target, len(c.Targets))
	var failed []string
	for name, tc := range c.Targets {
		tc.Name = name
		t, err := NewTarget(tc)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		targets[name] = t
	}
	if len(failed) > 0 {
		return nil, fmt.Errorf("cloudsync: %d target(s) failed: %s", len(failed), strings.Join(failed, "; "))
	}
	return targets, nil
}

// SortedNames returns target names in sorted order (stable output).
func (c *Config) SortedNames() []string {
	names := make([]string, 0, len(c.Targets))
	for name := range c.Targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IntOption returns options[key] parsed as int, or def when absent.
func (tc *TargetConfig) IntOption(key string, def int) (int, error) {
	v, ok := tc.Options[key]
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("option %q must be an integer, got %q", key, v)
	}
	return n, nil
}

// DurationOption returns options[key] parsed as a Go duration ("5s") or a
// bare number of seconds, or def when absent.
func (tc *TargetConfig) DurationOption(key string, def time.Duration) (time.Duration, error) {
	v, ok := tc.Options[key]
	if !ok || v == "" {
		return def, nil
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d, nil
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		return time.Duration(secs * float64(time.Second)), nil
	}
	return 0, fmt.Errorf("option %q must be a duration (e.g. \"30s\"), got %q", key, v)
}

// DefaultConfigPaths returns the search order for the config file.
func DefaultConfigPaths() []string {
	var paths []string
	if env := os.Getenv("CLOUDSYNC_CONFIG"); env != "" {
		paths = append(paths, env)
	}
	paths = append(paths,
		filepath.Join(".", "cloudsync.yaml"),
		filepath.Join(".", "cloudsync.yml"),
		filepath.Join(".", "cloudsync.json"),
	)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths,
			filepath.Join(home, ".cloudsync.yaml"),
			filepath.Join(home, ".config", "cloudsync", "cloudsync.yaml"),
		)
	}
	return paths
}

// FindConfig returns the first existing config path, or "".
func FindConfig() string {
	for _, p := range DefaultConfigPaths() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}
