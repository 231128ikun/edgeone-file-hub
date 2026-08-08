package cloudsync

import "strings"

// Redactor masks secret values inside strings (URLs, error messages, logs).
type Redactor struct {
	secrets []string
}

// NewRedactor collects non-empty secrets to mask.
func NewRedactor(secrets ...string) *Redactor {
	var kept []string
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if s != "" && s != "***" {
			kept = append(kept, s)
		}
	}
	return &Redactor{secrets: kept}
}

// Redact replaces every occurrence of a known secret with ***.
func (r *Redactor) Redact(s string) string {
	if r == nil || s == "" {
		return s
	}
	for _, sec := range r.secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return s
}

// RedactConfigSecrets returns a redactor built from the tokens of cfg.
func RedactConfigSecrets(cfg *Config) *Redactor {
	var secrets []string
	if cfg == nil {
		return NewRedactor()
	}
	for _, tc := range cfg.Targets {
		secrets = append(secrets, tc.Token)
	}
	return NewRedactor(secrets...)
}
