// Package httpprov implements the §7 edge-function protocol shared by all
// HTTP-based providers:
//
//	GET    /<key>                  → plain body (public, no auth)
//	PUT    /<key>  x-token: <token> Body: raw bytes → {"url","size"}
//	DELETE /<key>  x-token: <token>               → {"ok":true}
//
// cfkv and edgeone wrap this client and only differ in how they build keys.
package httpprov

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cloudsync/internal/cloudsync"
)

// Config configures the protocol client.
type Config struct {
	Name    string
	BaseURL string
	Token   string
	// KeyFor maps a sanitized logical filename to the storage key.
	// It may transform the name (EdgeOne KV charset) or add prefixes.
	KeyFor func(filename string) (string, error)
	// Verify enables read-back verification (default true).
	Verify bool
	// MaxRetries is the retry count for idempotent HTTP calls (default 3).
	MaxRetries int
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Client implements cloudsync.Target over HTTP.
type Client struct {
	cfg    Config
	base   *url.URL
	http   *http.Client
	policy cloudsync.Policy
}

// New validates cfg and returns a Client.
func New(cfg Config) (*Client, error) {
	if cfg.Name == "" {
		cfg.Name = "target"
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("cloudsync: base_url is required")
	}
	if cfg.KeyFor == nil {
		cfg.KeyFor = func(f string) (string, error) { return f, nil }
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("cloudsync: invalid base_url: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("cloudsync: base_url must be http(s), got %q", base.Scheme)
	}
	if base.Host == "" {
		return nil, errors.New("cloudsync: base_url is missing a host")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	return &Client{
		cfg:    cfg,
		base:   base,
		http:   hc,
		policy: cloudsync.Policy{MaxRetries: cfg.MaxRetries},
	}, nil
}

// Name returns the configured target name.
func (c *Client) Name() string { return c.cfg.Name }

// VerifyEnabled reports whether read-back verification should run.
func (c *Client) VerifyEnabled() bool { return c.cfg.Verify }

func (c *Client) key(filename string) (string, error) {
	return c.cfg.KeyFor(filename)
}

func (c *Client) urlForKey(key string) string {
	return c.base.JoinPath(key).String()
}

// Push uploads data and returns the public URL from the server response
// (falling back to base_url + key when the server returns no body).
func (c *Client) Push(ctx context.Context, filename string, data []byte) (string, error) {
	if c.cfg.Token == "" {
		return "", cloudsync.ErrTokenRequired
	}
	key, err := c.key(filename)
	if err != nil {
		return "", err
	}
	reqURL := c.urlForKey(key)
	var result string
	err = cloudsync.Retry(ctx, c.policy, func() error {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(data))
		if rerr != nil {
			return rerr
		}
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		req.Header.Set("x-token", c.cfg.Token)
		resp, derr := c.http.Do(req)
		if derr != nil {
			return derr
		}
		defer resp.Body.Close()
		body, berr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if berr != nil {
			return berr
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return fmt.Errorf("%w (http %d): %s", cloudsync.ErrUnauthorized, resp.StatusCode, bodySnippet(body))
		}
		if resp.StatusCode >= 400 {
			return bodyErr(resp, body)
		}
		var out struct {
			URL  string `json:"url"`
			Size int64  `json:"size"`
		}
		if len(bytes.TrimSpace(body)) > 0 && json.Unmarshal(body, &out) != nil {
			out = struct {
				URL  string `json:"url"`
				Size int64  `json:"size"`
			}{}
		}
		if out.URL == "" {
			out.URL = reqURL
		}
		result = out.URL
		return nil
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// Read fetches the remote object bytes.
func (c *Client) Read(ctx context.Context, filename string) ([]byte, error) {
	key, err := c.key(filename)
	if err != nil {
		return nil, err
	}
	reqURL := c.urlForKey(key)
	var result []byte
	err = cloudsync.Retry(ctx, c.policy, func() error {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if rerr != nil {
			return rerr
		}
		resp, derr := c.http.Do(req)
		if derr != nil {
			return derr
		}
		defer resp.Body.Close()
		body, berr := io.ReadAll(io.LimitReader(resp.Body, cloudsync.MaxPayloadBytes+1))
		if berr != nil {
			return berr
		}
		if resp.StatusCode == 404 {
			return cloudsync.ErrNotFound
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return fmt.Errorf("%w: http %d", cloudsync.ErrUnauthorized, resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			return bodyErr(resp, body)
		}
		if len(body) > cloudsync.MaxPayloadBytes {
			return fmt.Errorf("%w: %d bytes", cloudsync.ErrPayloadTooLarge, len(body))
		}
		result = body
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Delete removes the remote object.
func (c *Client) Delete(ctx context.Context, filename string) error {
	if c.cfg.Token == "" {
		return cloudsync.ErrTokenRequired
	}
	key, err := c.key(filename)
	if err != nil {
		return err
	}
	reqURL := c.urlForKey(key)
	return cloudsync.Retry(ctx, c.policy, func() error {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
		if rerr != nil {
			return rerr
		}
		req.Header.Set("x-token", c.cfg.Token)
		resp, derr := c.http.Do(req)
		if derr != nil {
			return derr
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode == 404 {
			return cloudsync.ErrNotFound
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return cloudsync.ErrUnauthorized
		}
		if resp.StatusCode >= 400 {
			return &cloudsync.HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
		}
		return nil
	})
}

func bodyErr(resp *http.Response, body []byte) error {
	return &cloudsync.HTTPError{StatusCode: resp.StatusCode, Status: resp.Status, Body: string(body)}
}

func bodySnippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
