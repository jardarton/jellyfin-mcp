// Command jellyfin-mcp serves a Jellyfin server as MCP tools, over stdio or
// Streamable HTTP.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jardarton/jellyfin-mcp/internal/jellyfin"
	"github.com/jardarton/jellyfin-mcp/internal/tools"
)

var version = "dev"

const usage = `jellyfin-mcp serves a Jellyfin server as MCP tools.

Usage: jellyfin-mcp [-transport stdio|http] [-addr :8080]

Configuration comes from the environment:

  JELLYFIN_URL                server URL, such as http://jellyfin:8096 (required)
  JELLYFIN_API_KEY            API key from Dashboard > API Keys (required)
  JELLYFIN_API_KEY_FILE       file holding the API key, instead of JELLYFIN_API_KEY
  JELLYFIN_USER               user name or id that tools act for when a call names none
  JELLYFIN_MCP_TRANSPORT      stdio (default) or http
  JELLYFIN_MCP_ADDR           listen address for http, default :8080
  JELLYFIN_MCP_AUTH_TOKEN     require "Authorization: Bearer <token>" on http
  JELLYFIN_MCP_AUTH_TOKEN_FILE  file holding the bearer token
  JELLYFIN_MCP_ALLOWED_HOSTS  comma-separated host names http answers to, against DNS rebinding
  JELLYFIN_MCP_READ_ONLY      true to leave out every tool that changes the server

Over http, MCP is served at /mcp and a health check at /healthz.
`

func main() {
	if err := run(); err != nil {
		slog.Error("jellyfin-mcp stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	flags := flag.NewFlagSet("jellyfin-mcp", flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprint(flags.Output(), usage) }
	transport := flags.String("transport", env("JELLYFIN_MCP_TRANSPORT", "stdio"), "stdio or http")
	addr := flags.String("addr", env("JELLYFIN_MCP_ADDR", ":8080"), "listen address for http")
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	baseURL := os.Getenv("JELLYFIN_URL")
	if baseURL == "" {
		return errors.New("JELLYFIN_URL is not set")
	}
	key, err := secret("JELLYFIN_API_KEY")
	if err != nil {
		return err
	}
	if key == "" {
		return errors.New("JELLYFIN_API_KEY is not set")
	}
	readOnly, err := boolEnv("JELLYFIN_MCP_READ_ONLY")
	if err != nil {
		return err
	}
	c, err := jellyfin.New(baseURL, key, version, nil)
	if err != nil {
		return err
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "jellyfin-mcp", Version: version},
		&mcp.ServerOptions{Instructions: tools.Instructions})
	tools.Register(server, c, tools.Options{
		DefaultUser: os.Getenv("JELLYFIN_USER"),
		ReadOnly:    readOnly,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch *transport {
	case "stdio":
		return server.Run(ctx, &mcp.StdioTransport{})
	case "http":
		token, err := secret("JELLYFIN_MCP_AUTH_TOKEN")
		if err != nil {
			return err
		}
		hosts := strings.FieldsFunc(os.Getenv("JELLYFIN_MCP_ALLOWED_HOSTS"), func(r rune) bool { return r == ',' || r == ' ' })
		return serveHTTP(ctx, *addr, server, token, hosts, readOnly)
	}
	return fmt.Errorf("unknown transport %q; use stdio or http", *transport)
}

func serveHTTP(ctx context.Context, addr string, server *mcp.Server, token string, hosts []string, readOnly bool) error {
	var h http.Handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	if token != "" {
		h = requireToken(token, h)
	} else {
		slog.Warn("serving MCP without authentication; set JELLYFIN_MCP_AUTH_TOKEN unless only trusted clients can reach it")
	}
	if len(hosts) > 0 {
		h = allowHosts(hosts, h)
	}
	h = http.NewCrossOriginProtection().Handler(h)
	mux := http.NewServeMux()
	mux.Handle("/mcp", h)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("jellyfin-mcp listening", "addr", addr, "version", version, "auth", token != "", "allowed_hosts", hosts, "read_only", readOnly)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// requireToken rejects requests without "Authorization: Bearer <token>".
func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowHosts rejects requests whose Host header is not one of hosts, so a
// browser can't be pointed at the server through a rebound DNS name.
func allowHosts(hosts []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
		if !slices.ContainsFunc(hosts, func(h string) bool { return strings.EqualFold(h, host) }) {
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func boolEnv(key string) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s=%q is not true or false", key, v)
	}
	return b, nil
}

// secret reads key from the environment, or from the file named by key_FILE.
func secret(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v), nil
	}
	path := os.Getenv(key + "_FILE")
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s_FILE: %w", key, err)
	}
	return strings.TrimSpace(string(b)), nil
}
