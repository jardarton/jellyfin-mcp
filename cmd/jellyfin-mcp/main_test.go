package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRequireToken(t *testing.T) {
	h := requireToken("tok", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	for header, want := range map[string]int{
		"":             http.StatusUnauthorized,
		"Bearer nope":  http.StatusUnauthorized,
		"tok":          http.StatusUnauthorized,
		"Bearer tok":   http.StatusTeapot,
		"Bearer tok ":  http.StatusUnauthorized,
		"Bearer token": http.StatusUnauthorized,
	} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Authorization %q: %d, want %d", header, w.Code, want)
		}
	}
}

func TestSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET", "")
	t.Setenv("TEST_SECRET_FILE", path)
	if got, err := secret("TEST_SECRET"); err != nil || got != "from-file" {
		t.Errorf("from file: %q, %v", got, err)
	}
	t.Setenv("TEST_SECRET", "from-env")
	if got, _ := secret("TEST_SECRET"); got != "from-env" {
		t.Errorf("env should win: %q", got)
	}
	t.Setenv("TEST_SECRET", "")
	t.Setenv("TEST_SECRET_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := secret("TEST_SECRET"); err == nil {
		t.Error("missing file succeeded")
	}
}

func TestLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true,
		"[::1]:8080":     true,
		"localhost:8080": true,
		":8080":          false,
		"0.0.0.0:8080":   false,
		"[::]:8080":      false,
		"10.0.0.5:8080":  false,
		"example:8080":   false,
		"garbage":        false,
	} {
		if got := loopback(addr); got != want {
			t.Errorf("loopback(%q) = %v", addr, got)
		}
	}
}

func TestAllowHosts(t *testing.T) {
	h := allowHosts([]string{"jellyfin-mcp", "mcp.example.com", "::1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	for host, want := range map[string]int{
		"jellyfin-mcp:8080":     http.StatusTeapot,
		"JELLYFIN-MCP":          http.StatusTeapot,
		"mcp.example.com.":      http.StatusTeapot,
		"[::1]:8080":            http.StatusTeapot,
		"attacker.example:8080": http.StatusForbidden,
		"10.0.0.5:8080":         http.StatusForbidden,
	} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Host %q: %d, want %d", host, w.Code, want)
		}
	}
}
