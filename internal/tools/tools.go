// Package tools exposes a Jellyfin server as MCP tools for an agent.
package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jardarton/jellyfin-mcp/internal/jellyfin"
)

// Instructions tells the agent how to use the server as a whole.
const Instructions = `jellyfin-mcp reaches one Jellyfin media server: its movies, shows, music, books and the people watching them.
- Find things with find_items; most tools also take an item by name, with an optional "(year)", but an id from a previous result is exact.
- Watch state, favorites, continue watching and next up belong to a Jellyfin user. Pass user (a name from "users") when the server has more than one user and no default is configured.
- Remote control works on sessions that support it ("sessions" shows them). Say which device you are about to control before playing, pausing or stopping something someone may be watching.
- Times are h:mm:ss, runtimes minutes, dates YYYY-MM-DD and timestamps UTC.`

// Options configure the tools.
type Options struct {
	// DefaultUser is the user name or id used when a call names no user.
	DefaultUser string
	// ReadOnly leaves out every tool that changes the server.
	ReadOnly bool
}

// Tools serves the MCP tools.
type Tools struct {
	c    *jellyfin.Client
	opts Options
}

// Register adds the tools to server.
func Register(server *mcp.Server, c *jellyfin.Client, opts Options) {
	t := &Tools{c: c, opts: opts}
	t.registerLibrary(server)
	t.registerUsers(server)
	t.registerPlaylists(server)
	t.registerSessions(server)
	t.registerServer(server)
}

type tool struct {
	name, description string
	// enums restricts string properties (or the items of string array
	// properties) to the given values.
	enums       map[string][]string
	readOnly    bool
	destructive bool
}

func add[In, Out any](t *Tools, server *mcp.Server, tl tool, h func(context.Context, In) (Out, error)) {
	if t.opts.ReadOnly && !tl.readOnly {
		return
	}
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("%s: %v", tl.name, err))
	}
	for prop, vals := range tl.enums {
		p := schema.Properties[prop]
		if p == nil {
			panic(fmt.Sprintf("%s: no property %q", tl.name, prop))
		}
		if p.Items != nil {
			p = p.Items
		}
		for _, v := range vals {
			p.Enum = append(p.Enum, v)
		}
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        tl.name,
		Description: tl.description,
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    tl.readOnly,
			DestructiveHint: &tl.destructive,
			OpenWorldHint:   new(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := h(ctx, in)
		return nil, out, err
	})
}

// item is an item in a list.
type item struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	Year           int        `json:"year,omitempty"`
	Series         string     `json:"series,omitempty"`
	Season         *int       `json:"season,omitempty"`
	Episode        *int       `json:"episode,omitempty"`
	Album          string     `json:"album,omitempty"`
	Artist         string     `json:"artist,omitempty"`
	RuntimeMinutes int        `json:"runtime_minutes,omitempty"`
	Rating         float64    `json:"rating,omitempty" jsonschema:"community rating out of 10"`
	OfficialRating string     `json:"official_rating,omitempty"`
	Genres         []string   `json:"genres,omitempty"`
	PremiereDate   string     `json:"premiere_date,omitempty"`
	DateAdded      string     `json:"date_added,omitempty"`
	ChildCount     int        `json:"child_count,omitempty" jsonschema:"seasons of a series, episodes of a season, tracks of an album, items of a playlist or collection"`
	UserState      *userState `json:"user_state,omitempty"`
}

// userState is one user's state for an item.
type userState struct {
	Played         bool    `json:"played"`
	Favorite       bool    `json:"favorite"`
	PlayCount      int     `json:"play_count,omitempty"`
	ResumeAt       string  `json:"resume_at,omitempty" jsonschema:"where playback stopped, h:mm:ss"`
	PlayedPercent  float64 `json:"played_percent,omitempty"`
	UnplayedCount  *int    `json:"unplayed_count,omitempty" jsonschema:"unplayed episodes or tracks, for series, seasons and albums"`
	LastPlayedDate string  `json:"last_played,omitempty"`
}

func toItem(it jellyfin.Item) item {
	out := item{
		ID:             it.ID,
		Name:           it.Name,
		Type:           it.Type,
		Year:           it.ProductionYear,
		Series:         it.SeriesName,
		Album:          it.Album,
		Artist:         artist(it),
		RuntimeMinutes: minutes(it.RunTimeTicks),
		Rating:         round1(it.CommunityRating),
		OfficialRating: it.OfficialRating,
		Genres:         it.Genres,
		PremiereDate:   date(it.PremiereDate),
		DateAdded:      date(it.DateCreated),
		ChildCount:     it.ChildCount,
		UserState:      toUserState(it.UserData),
	}
	if it.Type == "Episode" {
		out.Season, out.Episode = it.ParentIndexNumber, it.IndexNumber
	}
	return out
}

func toItems(its []jellyfin.Item) []item {
	out := make([]item, len(its))
	for i, it := range its {
		out[i] = toItem(it)
	}
	return out
}

func toUserState(d *jellyfin.UserData) *userState {
	if d == nil {
		return nil
	}
	s := &userState{
		Played:         d.Played,
		Favorite:       d.IsFavorite,
		PlayCount:      d.PlayCount,
		UnplayedCount:  d.UnplayedItemCount,
		LastPlayedDate: timestamp(d.LastPlayedDate),
	}
	if d.PlaybackPositionTicks > 0 {
		s.ResumeAt = clock(d.PlaybackPositionTicks)
	}
	if d.PlayedPercentage != nil {
		s.PlayedPercent = round1(*d.PlayedPercentage)
	}
	return s
}

func artist(it jellyfin.Item) string {
	if it.AlbumArtist != "" {
		return it.AlbumArtist
	}
	return strings.Join(it.Artists, ", ")
}

func minutes(ticks int64) int {
	return int((ticks + 30*jellyfin.TicksPerSecond) / (60 * jellyfin.TicksPerSecond))
}

// clock formats ticks as h:mm:ss.
func clock(ticks int64) string {
	s := ticks / jellyfin.TicksPerSecond
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
}

// maxClock bounds positions parseClock accepts, in seconds, well below tick overflow.
const maxClock = 1000 * 3600

// parseClock reads h:mm:ss, m:ss or seconds into ticks.
func parseClock(s string) (int64, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("position %q is not h:mm:ss", s)
	}
	var secs int64
	for i, part := range parts {
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil || n < 0 || i > 0 && n >= 60 || len(part) > 7 {
			return 0, fmt.Errorf("position %q is not h:mm:ss", s)
		}
		secs = secs*60 + n
	}
	// At most 3 parts of 7 digits each, so secs can't overflow.
	if secs > maxClock {
		return 0, fmt.Errorf("position %q is too far in", s)
	}
	return secs * jellyfin.TicksPerSecond, nil
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

// date turns a Jellyfin timestamp into YYYY-MM-DD.
func date(s string) string {
	if t, ok := parseTime(s); ok {
		return t.Format(time.DateOnly)
	}
	return ""
}

// timestamp turns a Jellyfin timestamp into RFC 3339 UTC, to the second.
func timestamp(s string) string {
	if t, ok := parseTime(s); ok {
		return t.UTC().Format(time.RFC3339)
	}
	return ""
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// Jellyfin sometimes leaves out the zone; it is UTC.
		t, err = time.Parse("2006-01-02T15:04:05.9999999", s)
	}
	if err != nil || t.Year() <= 1 {
		return time.Time{}, false
	}
	return t, true
}

func clampLimit(n, def, max int) int {
	switch {
	case n <= 0:
		return def
	case n > max:
		return max
	}
	return n
}
