// Package jellyfin is a small client for the parts of the Jellyfin HTTP API
// that jellyfin-mcp uses. It targets Jellyfin 10.10 and later.
package jellyfin

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
	"sync"
	"time"
)

// Client calls one Jellyfin server with an API key or access token.
type Client struct {
	base *url.URL
	auth string
	http *http.Client

	mu      sync.Mutex
	users   []User
	usersAt time.Time
}

// New returns a client for the server at baseURL, which may include a path
// prefix such as https://example.com/jellyfin.
func New(baseURL, token, version string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse Jellyfin URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("jellyfin URL %q must be an absolute http(s) URL", baseURL)
	}
	if token == "" {
		return nil, errors.New("jellyfin API key is empty")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		base: u,
		auth: fmt.Sprintf(`MediaBrowser Client="jellyfin-mcp", Device="jellyfin-mcp", DeviceId="jellyfin-mcp", Version=%q, Token=%q`, version, token),
		http: hc,
	}, nil
}

// Error is a response with a non-2xx status.
type Error struct {
	Method, Path string
	Status       int
	Body         string
}

func (e *Error) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "jellyfin rejected the API key (401)"
	case http.StatusForbidden:
		return fmt.Sprintf("jellyfin refused %s %s (403); the API key or user lacks permission", e.Method, e.Path)
	case http.StatusNotFound:
		return fmt.Sprintf("jellyfin has no %s (404)", e.Path)
	}
	msg := fmt.Sprintf("jellyfin %s %s: %d %s", e.Method, e.Path, e.Status, http.StatusText(e.Status))
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// IsNotFound reports whether err is ErrNotFound or a 404 from Jellyfin.
func IsNotFound(err error) bool {
	var e *Error
	return errors.Is(err, ErrNotFound) || errors.As(err, &e) && e.Status == http.StatusNotFound
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	u := c.base.JoinPath(path)
	u.RawQuery = q.Encode()

	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("jellyfin %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &Error{Method: method, Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("jellyfin %s %s: decode response: %w", method, path, err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, q, nil, out)
}

func (c *Client) post(ctx context.Context, path string, q url.Values, body, out any) error {
	return c.do(ctx, http.MethodPost, path, q, body, out)
}

func (c *Client) delete(ctx context.Context, path string, q url.Values) error {
	return c.do(ctx, http.MethodDelete, path, q, nil, nil)
}
