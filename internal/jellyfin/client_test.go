package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	for _, bad := range []string{"", "jellyfin:8096", "ftp://example.com", "http://"} {
		if _, err := New(bad, "key", "test", nil); err == nil {
			t.Errorf("New(%q) succeeded", bad)
		}
	}
	if _, err := New("http://example.com", "", "test", nil); err == nil {
		t.Error("New without a key succeeded")
	}
}

func TestRequest(t *testing.T) {
	var got *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		switch r.URL.Path {
		case "/prefix/System/Info":
			w.Write([]byte(`{"ServerName":"test","Version":"10.11.0"}`))
		case "/prefix/Users":
			http.Error(w, "nope", http.StatusUnauthorized)
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	c, err := New(ts.URL+"/prefix/", "s3cret", "1.2.3", ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	info, err := c.SystemInfo(ctx)
	if err != nil || info.Version != "10.11.0" {
		t.Fatalf("SystemInfo = %+v, %v", info, err)
	}
	auth := got.Header.Get("Authorization")
	for _, want := range []string{`MediaBrowser `, `Client="jellyfin-mcp"`, `Version="1.2.3"`, `Token="s3cret"`} {
		if !strings.Contains(auth, want) {
			t.Errorf("Authorization %q lacks %s", auth, want)
		}
	}

	if _, err := c.Users(ctx); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Errorf("401: %v", err)
	}
	if err := c.RefreshLibrary(ctx); err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("500: %v", err)
	}
	if _, err := c.Item(ctx, "x", ""); err == nil {
		t.Error("Item on a failing server succeeded")
	}
}

func TestItemsQuery(t *testing.T) {
	played := false
	v := ItemsQuery{
		UserID:   "u",
		Types:    []string{"Movie", "Series"},
		Genres:   []string{"Drama", "Sci-Fi"},
		Years:    []int{1999, 2001},
		IsPlayed: &played,
		Limit:    5,
	}.values()
	for k, want := range map[string]string{
		"userId":           "u",
		"includeItemTypes": "Movie,Series",
		"genres":           "Drama|Sci-Fi",
		"years":            "1999,2001",
		"isPlayed":         "false",
		"limit":            "5",
		"enableImages":     "false",
	} {
		if got := v.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	for _, k := range []string{"searchTerm", "isFavorite", "startIndex", "recursive"} {
		if v.Has(k) {
			t.Errorf("%s set without a value", k)
		}
	}
}
