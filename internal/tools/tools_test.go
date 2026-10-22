package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jardarton/jellyfin-mcp/internal/jellyfin"
)

const (
	aliceID    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	bobID      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	movieID    = "11111111111111111111111111111111"
	remakeID   = "22222222222222222222222222222222"
	seriesID   = "33333333333333333333333333333333"
	playlistID = "44444444444444444444444444444444"
	songID     = "55555555555555555555555555555555"
	libraryID  = "66666666666666666666666666666666"
	apiKey     = "test-api-key"
)

// fakeJellyfin serves a tiny made-up library and records the requests that
// change something.
type fakeJellyfin struct {
	t        *testing.T
	users    []jellyfin.User
	items    []jellyfin.Item
	sessions []jellyfin.Session

	mu      sync.Mutex
	changes []string
	queries []url.Values // of GET /Items
}

func newFake(t *testing.T) *fakeJellyfin {
	alice := jellyfin.User{ID: aliceID, Name: "Alice"}
	bob := jellyfin.User{ID: bobID, Name: "Bob"}
	return &fakeJellyfin{
		t:     t,
		users: []jellyfin.User{alice, bob},
		items: []jellyfin.Item{
			{ID: movieID, Name: "The Example", Type: "Movie", ProductionYear: 1999, RunTimeTicks: 95 * 60 * jellyfin.TicksPerSecond},
			{ID: remakeID, Name: "The Example", Type: "Movie", ProductionYear: 2021},
			{ID: seriesID, Name: "Sample Show", Type: "Series", ProductionYear: 2010},
			{ID: playlistID, Name: "Road Trip", Type: "Playlist"},
			{ID: songID, Name: "Test Tone", Type: "Audio", AlbumArtist: "Nobody"},
		},
		sessions: []jellyfin.Session{
			{ID: "tv-session", DeviceName: "Living Room TV", Client: "Jellyfin Android TV", UserID: aliceID, UserName: "Alice", SupportsRemoteControl: true},
			{ID: "web-session", DeviceName: "Firefox", Client: "Jellyfin Web", UserID: bobID, UserName: "Bob"},
		},
	}
}

func (f *fakeJellyfin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if got, want := r.Header.Get("Authorization"), `Token="`+apiKey+`"`; !strings.Contains(got, want) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	path, ok := strings.CutPrefix(r.URL.Path, "/jf/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if r.Method != http.MethodGet {
		f.mu.Lock()
		f.changes = append(f.changes, r.Method+" "+path+"?"+q.Encode())
		f.mu.Unlock()
	}

	switch {
	case r.Method == http.MethodGet && path == "Users":
		f.json(w, f.users)
	case r.Method == http.MethodGet && path == "Items":
		f.mu.Lock()
		f.queries = append(f.queries, q)
		f.mu.Unlock()
		f.json(w, f.query(q))
	case r.Method == http.MethodGet && path == "System/Info":
		f.json(w, map[string]any{"ServerName": "test", "Version": "10.11.0", "SystemArchitecture": "X64", "HasUpdateAvailable": false})
	case r.Method == http.MethodGet && path == "Library/VirtualFolders":
		f.json(w, []jellyfin.VirtualFolder{{Name: "Movies", ItemID: libraryID, CollectionType: "movies"}})
	case r.Method == http.MethodGet && path == "Sessions":
		f.json(w, f.sessions)
	case r.Method == http.MethodGet && path == "Playlists/"+playlistID+"/Items":
		f.json(w, jellyfin.ItemsResult{Items: []jellyfin.Item{
			{ID: songID, Name: "Test Tone", Type: "Audio", PlaylistItemID: "entry-1"},
			{ID: movieID, Name: "The Example", Type: "Movie", ProductionYear: 1999, PlaylistItemID: "entry-2"},
			{ID: songID, Name: "Test Tone", Type: "Audio", PlaylistItemID: "entry-3"},
			{ID: remakeID, Name: "The Example", Type: "Movie", ProductionYear: 2021, PlaylistItemID: "entry-4"},
		}})
	case r.Method == http.MethodGet && path == "Shows/"+seriesID+"/Episodes":
		var eps []jellyfin.Item
		for i := 1; i <= 5; i++ {
			n := i
			eps = append(eps, jellyfin.Item{ID: strings.Repeat(strconv.Itoa(i), 32), Name: "Episode " + strconv.Itoa(i),
				Type: "Episode", SeriesName: "Sample Show", IndexNumber: &n, UserData: &jellyfin.UserData{Played: i <= 2}})
		}
		f.json(w, jellyfin.ItemsResult{Items: eps, TotalRecordCount: len(eps)})
	case strings.HasPrefix(path, "UserPlayedItems/"):
		f.json(w, jellyfin.UserData{Played: r.Method == http.MethodPost})
	case r.Method == http.MethodPost && path == "Playlists":
		f.json(w, map[string]string{"Id": playlistID})
	case r.Method != http.MethodGet:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// query implements the parts of GET /Items the tools use.
func (f *fakeJellyfin) query(q map[string][]string) jellyfin.ItemsResult {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	var out []jellyfin.Item
	for _, it := range f.items {
		if ids := get("ids"); ids != "" && !slices.Contains(strings.Split(ids, ","), it.ID) {
			continue
		}
		if s := get("searchTerm"); s != "" && !strings.Contains(strings.ToLower(it.Name), strings.ToLower(s)) {
			continue
		}
		if ts := get("includeItemTypes"); ts != "" && !slices.Contains(strings.Split(ts, ","), it.Type) {
			continue
		}
		if y := get("years"); y != "" && y != strconv.Itoa(it.ProductionYear) {
			continue
		}
		if get("userId") != "" {
			it.UserData = &jellyfin.UserData{}
		}
		out = append(out, it)
	}
	return jellyfin.ItemsResult{Items: out, TotalRecordCount: len(out)}
}

func (f *fakeJellyfin) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		f.t.Error(err)
	}
}

// lastQuery returns the last GET /Items query.
func (f *fakeJellyfin) lastQuery() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[len(f.queries)-1]
}

func (f *fakeJellyfin) changed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.changes)
}

type client struct {
	t  *testing.T
	cs *mcp.ClientSession
}

func newClient(t *testing.T, f *fakeJellyfin, opts Options) *client {
	t.Helper()
	ctx := context.Background()
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	c, err := jellyfin.New(ts.URL+"/jf/", apiKey, "test", ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "jellyfin-mcp"}, &mcp.ServerOptions{Instructions: Instructions})
	Register(server, c, opts)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return &client{t: t, cs: cs}
}

// call calls a tool and decodes its structured result into out, returning
// the error text if the tool failed.
func (c *client) call(name string, args any, out any) string {
	c.t.Helper()
	res, err := c.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		c.t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		return res.Content[0].(*mcp.TextContent).Text
	}
	if out != nil {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			c.t.Fatalf("%s: decoding %s: %v", name, b, err)
		}
	}
	return ""
}

func (c *client) mustCall(name string, args any, out any) {
	c.t.Helper()
	if msg := c.call(name, args, out); msg != "" {
		c.t.Fatalf("%s: %s", name, msg)
	}
}

func TestItemByName(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f, Options{})

	var it itemDetails
	c.mustCall("get_item", map[string]any{"item": "the example (2021)"}, &it)
	if it.ID != remakeID {
		t.Errorf("year picked %s, want the 2021 one", it.ID)
	}
	c.mustCall("get_item", map[string]any{"item": "sample"}, &it)
	if it.ID != seriesID {
		t.Errorf("only partial match picked %s, want the series", it.ID)
	}
	c.mustCall("get_item", map[string]any{"item": movieID}, &it)
	if it.Name != "The Example" || it.RuntimeMinutes != 95 {
		t.Errorf("by id: %+v", it.item)
	}

	if f := f.lastQuery().Get("fields"); !strings.Contains(f, "OriginalTitle") {
		t.Errorf("get_item by id asked for fields %q", f)
	}
	msg := c.call("get_item", map[string]any{"item": "The Example"}, nil)
	if f := f.lastQuery().Get("fields"); f != "OriginalTitle" {
		t.Errorf("name lookup asked for fields %q", f)
	}
	if !strings.Contains(msg, movieID) || !strings.Contains(msg, remakeID) {
		t.Errorf("ambiguous name: %q should list both ids", msg)
	}
	if msg := c.call("get_item", map[string]any{"item": "Nothing Like It"}, nil); !strings.Contains(msg, `no item named "Nothing Like It"`) {
		t.Errorf("missing name: %q", msg)
	}
	if msg := c.call("get_item", map[string]any{"item": strings.Repeat("9", 32)}, nil); !strings.Contains(msg, "no item with id") {
		t.Errorf("missing id: %q", msg)
	}
}

func TestUser(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f, Options{})

	msg := c.call("continue_watching", map[string]any{}, nil)
	if !strings.Contains(msg, "Alice") || !strings.Contains(msg, "Bob") {
		t.Errorf("with two users and no default: %q should name both", msg)
	}
	if msg := c.call("continue_watching", map[string]any{"user": "carol"}, nil); !strings.Contains(msg, `no user "carol"`) {
		t.Errorf("unknown user: %q", msg)
	}
	// Searching doesn't need a user.
	var out itemsOut
	c.mustCall("find_items", map[string]any{"query": "sample"}, &out)
	if len(out.Items) != 1 || out.Items[0].UserState != nil {
		t.Errorf("find_items without user: %+v", out)
	}

	c = newClient(t, f, Options{DefaultUser: "bob"})
	var users usersOut
	c.mustCall("users", map[string]any{}, &users)
	if users.DefaultUser != "Bob" {
		t.Errorf("default user %q, want Bob", users.DefaultUser)
	}
	var changed changedOut
	c.mustCall("mark_played", map[string]any{"items": []string{"Sample Show"}}, &changed)
	if changed.User != "Bob" || !changed.Items[0].UserState.Played {
		t.Errorf("mark_played: %+v", changed)
	}
	c.mustCall("mark_unplayed", map[string]any{"items": []string{"The Example (1999)"}, "user": "alice"}, &changed)
	if changed.User != "Alice" || changed.Items[0].UserState.Played {
		t.Errorf("mark_unplayed: %+v", changed)
	}
	if got, want := f.changed(), []string{
		"POST UserPlayedItems/" + seriesID + "?userId=" + bobID,
		"DELETE UserPlayedItems/" + movieID + "?userId=" + aliceID,
	}; !slices.Equal(got, want) {
		t.Errorf("requests %q, want %q", got, want)
	}
}

func TestControlPlayback(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f, Options{})

	var out controlOut
	c.mustCall("control_playback", map[string]any{"session": "living room tv", "command": "play", "items": []string{"Sample Show"}, "position": "1:00"}, &out)
	if out.Sent != "PlayNow" || out.Session.ID != "tv-session" {
		t.Errorf("play: %+v", out)
	}
	c.mustCall("control_playback", map[string]any{"session": "tv-session", "command": "seek", "position": "1:02:03"}, &out)
	c.mustCall("control_playback", map[string]any{"session": "Living Room TV", "command": "toggle_pause"}, nil)
	want := []string{
		"POST Sessions/tv-session/Playing?itemIds=" + seriesID + "&playCommand=PlayNow&startPositionTicks=600000000",
		"POST Sessions/tv-session/Playing/Seek?seekPositionTicks=37230000000",
		"POST Sessions/tv-session/Playing/PlayPause?",
	}
	if got := f.changed(); !slices.Equal(got, want) {
		t.Errorf("requests\n%q\nwant\n%q", got, want)
	}

	if msg := c.call("control_playback", map[string]any{"session": "Firefox", "command": "pause"}, nil); !strings.Contains(msg, "does not support remote control") {
		t.Errorf("non-remote session: %q", msg)
	}
	if msg := c.call("control_playback", map[string]any{"session": "tv-session", "command": "seek"}, nil); !strings.Contains(msg, "needs a position") {
		t.Errorf("seek without position: %q", msg)
	}
}

func TestRemoveFromPlaylist(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f, Options{DefaultUser: "Alice"})

	c.mustCall("remove_from_playlist", map[string]any{"playlist": "road trip", "items": []string{"test tone"}}, nil)
	c.mustCall("remove_from_playlist", map[string]any{"playlist": "Road Trip", "items": []string{"The Example (2021)", movieID}}, nil)
	want := []string{
		"DELETE Playlists/" + playlistID + "/Items?entryIds=entry-1%2Centry-3",
		"DELETE Playlists/" + playlistID + "/Items?entryIds=entry-4%2Centry-2",
	}
	if got := f.changed(); !slices.Equal(got, want) {
		t.Errorf("requests %q, want %q", got, want)
	}
	if msg := c.call("remove_from_playlist", map[string]any{"playlist": "Road Trip", "items": []string{"Test Tone", "   "}}, nil); !strings.Contains(msg, "blank") {
		t.Errorf("blank item: %q", msg)
	}
	if got := f.changed(); len(got) != len(want) {
		t.Errorf("a refused removal still sent %q", got[len(want):])
	}
	msg := c.call("remove_from_playlist", map[string]any{"playlist": "Road Trip", "items": []string{"The Example"}}, nil)
	if !strings.Contains(msg, "several items match") || !strings.Contains(msg, movieID) || !strings.Contains(msg, remakeID) {
		t.Errorf("two different items with one name: %q", msg)
	}
	if msg := c.call("remove_from_playlist", map[string]any{"playlist": "Road Trip", "items": []string{"Sample Show"}}, nil); !strings.Contains(msg, "is not in playlist") {
		t.Errorf("item not in playlist: %q", msg)
	}
	if msg := c.call("add_to_playlist", map[string]any{"playlist": "Sample Show", "items": []string{"Test Tone"}}, nil); !strings.Contains(msg, `no playlist named "Sample Show"`) {
		t.Errorf("playlist by a series' name: %q", msg)
	}
	if msg := c.call("add_to_playlist", map[string]any{"playlist": seriesID, "items": []string{"Test Tone"}}, nil); !strings.Contains(msg, "not a playlist") {
		t.Errorf("playlist by a series' id: %q", msg)
	}
}

func TestReadOnly(t *testing.T) {
	ctx := context.Background()
	list := func(opts Options) map[string]*mcp.Tool {
		c := newClient(t, newFake(t), opts)
		res, err := c.cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]*mcp.Tool{}
		for _, tl := range res.Tools {
			m[tl.Name] = tl
		}
		return m
	}
	all, ro := list(Options{}), list(Options{ReadOnly: true})
	if len(ro) == 0 || len(ro) >= len(all) {
		t.Fatalf("read-only has %d of %d tools", len(ro), len(all))
	}
	for name, tl := range all {
		_, kept := ro[name]
		if kept != tl.Annotations.ReadOnlyHint {
			t.Errorf("%s: readOnlyHint %v but kept in read-only mode %v", name, tl.Annotations.ReadOnlyHint, kept)
		}
		destructive := slices.Contains([]string{"mark_played", "mark_unplayed", "refresh_metadata", "remove_from_playlist", "run_task"}, name)
		if *tl.Annotations.DestructiveHint != destructive {
			t.Errorf("%s: destructiveHint %v", name, *tl.Annotations.DestructiveHint)
		}
	}
}

func TestClock(t *testing.T) {
	for _, tc := range []struct {
		in   string
		secs int64
	}{{"45", 45}, {"1:30", 90}, {"1:02:03", 3723}, {" 0:00:07 ", 7}, {"1000:00:00", 3_600_000}} {
		got, err := parseClock(tc.in)
		if err != nil || got != tc.secs*jellyfin.TicksPerSecond {
			t.Errorf("parseClock(%q) = %d, %v; want %d s", tc.in, got, err, tc.secs)
		}
	}
	for _, bad := range []string{"", "1:xx", "-5", "1::2", "1m30s", "1:90", "1:2:3:4", "99999999999999", "1000:00:01", "3000000:00:00"} {
		if _, err := parseClock(bad); err == nil {
			t.Errorf("parseClock(%q) succeeded", bad)
		}
	}
	if got := clock(3723 * jellyfin.TicksPerSecond); got != "1:02:03" {
		t.Errorf("clock = %q", got)
	}
}

func TestTimes(t *testing.T) {
	if got := timestamp("2024-05-06T07:08:09.1234567Z"); got != "2024-05-06T07:08:09Z" {
		t.Errorf("timestamp = %q", got)
	}
	if got := date("2024-05-06T00:00:00.0000000"); got != "2024-05-06" {
		t.Errorf("date without zone = %q", got)
	}
	if got := date("0001-01-01T00:00:00.0000000Z"); got != "" {
		t.Errorf("zero date = %q", got)
	}

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"":                     now.AddDate(0, 0, -7),
		"24h":                  now.Add(-24 * time.Hour),
		"2026-02-01":           time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		"2026-02-01T10:00:00Z": time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
	} {
		got, err := parseSince(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseSince(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseSince("last week", now); err == nil {
		t.Error("parseSince accepted prose")
	}
}

func TestFindItemsSort(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f, Options{})
	for _, args := range []map[string]any{{"query": "example", "sort": "rating"}, {"query": "example", "order": "desc"}} {
		if msg := c.call("find_items", args, nil); !strings.Contains(msg, "don't apply with query") {
			t.Errorf("%v: %q", args, msg)
		}
	}
	for _, tc := range []struct {
		args      map[string]any
		by, order string
	}{
		{map[string]any{"types": []string{"Movie"}}, "SortName", "Ascending"},
		{map[string]any{"sort": "rating"}, "CommunityRating,SortName", "Descending"},
		{map[string]any{"sort": "rating", "order": "asc"}, "CommunityRating,SortName", "Ascending"},
		{map[string]any{"query": "example"}, "", ""},
	} {
		c.mustCall("find_items", tc.args, nil)
		q := f.lastQuery()
		if q.Get("sortBy") != tc.by || q.Get("sortOrder") != tc.order {
			t.Errorf("%v: sortBy %q sortOrder %q, want %q %q", tc.args, q.Get("sortBy"), q.Get("sortOrder"), tc.by, tc.order)
		}
	}
}

func TestServerInfo(t *testing.T) {
	c := newClient(t, newFake(t), Options{})
	var out map[string]any
	c.mustCall("server_info", map[string]any{}, &out)
	for _, k := range []string{"architecture", "os", "update_available"} {
		if _, ok := out[k]; ok {
			t.Errorf("server_info reports %s, which Jellyfin no longer fills in", k)
		}
	}
	if out["version"] != "10.11.0" {
		t.Errorf("server_info: %v", out)
	}
}

func TestEpisodesPaging(t *testing.T) {
	c := newClient(t, newFake(t), Options{DefaultUser: "Alice"})

	var out episodesOut
	c.mustCall("episodes", map[string]any{"series": "Sample Show", "limit": 2}, &out)
	if len(out.Episodes) != 2 || out.Total != 5 || out.Unplayed != 3 || out.NextOffset != 2 {
		t.Errorf("first page: %d episodes, total %d, unplayed %d, next %d", len(out.Episodes), out.Total, out.Unplayed, out.NextOffset)
	}
	out = episodesOut{}
	c.mustCall("episodes", map[string]any{"series": "Sample Show", "limit": 2, "offset": 4}, &out)
	if len(out.Episodes) != 1 || *out.Episodes[0].Episode != 5 || out.NextOffset != 0 {
		t.Errorf("last page: %+v", out)
	}
	out = episodesOut{}
	c.mustCall("episodes", map[string]any{"series": "Sample Show", "unplayed_only": true}, &out)
	if len(out.Episodes) != 3 || out.Total != 3 || *out.Episodes[0].Episode != 3 {
		t.Errorf("unplayed only: %+v", out)
	}
}
