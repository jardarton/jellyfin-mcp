package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jardarton/jellyfin-mcp/internal/jellyfin"
)

// sorts maps find_items sort names to Jellyfin's sortBy and default order.
var sorts = map[string]struct{ by, order string }{
	"name":          {"SortName", "Ascending"},
	"date_added":    {"DateCreated,SortName", "Descending"},
	"release_date":  {"PremiereDate,ProductionYear,SortName", "Descending"},
	"rating":        {"CommunityRating,SortName", "Descending"},
	"critic_rating": {"CriticRating,SortName", "Descending"},
	"runtime":       {"Runtime,SortName", "Ascending"},
	"play_count":    {"PlayCount,SortName", "Descending"},
	"last_played":   {"DatePlayed,SortName", "Descending"},
	"random":        {"Random", "Ascending"},
}

func (t *Tools) registerLibrary(server *mcp.Server) {
	add(t, server, tool{
		name: "find_items",
		description: "Search or browse the library. Filter by words in the title, type, library, genre, year, person, " +
			"and, for the user, watched state and favorites. A query is matched against titles and ordered by relevance; " +
			"without one it lists everything that matches the filters, sorted as asked. " +
			"Returns a page of items and the total, so use offset to read on.",
		enums: map[string][]string{
			"types": searchTypes,
			"sort":  {"name", "date_added", "release_date", "rating", "critic_rating", "runtime", "play_count", "last_played", "random"},
			"order": {"asc", "desc"},
		},
		readOnly: true,
	}, t.findItems)
	add(t, server, tool{
		name: "get_item",
		description: "Everything about one item: overview, genres, cast and crew, studios, ratings, external ids, " +
			"video, audio and subtitle tracks, file path, and the user's watched state.",
		readOnly: true,
	}, t.getItem)
	add(t, server, tool{
		name:        "libraries",
		description: "The server's libraries (name, id, content type, folders) and how many movies, series, episodes, albums, songs and books it has.",
		readOnly:    true,
	}, t.libraries)
	add(t, server, tool{
		name:        "latest",
		description: "Recently added items, newest first, grouped like the Jellyfin home screen (new episodes of one series count as one entry).",
		enums:       map[string][]string{"types": searchTypes},
		readOnly:    true,
	}, t.latest)
	add(t, server, tool{
		name:        "continue_watching",
		description: "The user's partly watched or listened items, most recent first, with where they stopped.",
		enums:       map[string][]string{"types": searchTypes},
		readOnly:    true,
	}, t.continueWatching)
	add(t, server, tool{
		name:        "next_up",
		description: "The next unwatched episode of each series the user is watching, or of one series.",
		readOnly:    true,
	}, t.nextUp)
	add(t, server, tool{
		name:        "episodes",
		description: "A series' episodes in order, with season and episode numbers, air dates and the user's watched state.",
		readOnly:    true,
	}, t.episodes)
	add(t, server, tool{
		name:        "similar",
		description: "Items in the library similar to one item, for recommendations.",
		readOnly:    true,
	}, t.similar)
}

type userIn struct {
	User string `json:"user,omitempty" jsonschema:"Jellyfin user name or id; omit for the default user"`
}

// optionalUser resolves the user when one can be found; without a user
// reference and with several users to choose from it returns "".
func (t *Tools) optionalUser(ctx context.Context, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" && t.opts.DefaultUser == "" {
		us, err := t.c.Users(ctx)
		if err != nil {
			return "", err
		}
		enabled := 0
		for _, u := range us {
			if !u.Policy.IsDisabled {
				enabled++
			}
		}
		if enabled != 1 {
			return "", nil
		}
	}
	u, err := t.user(ctx, ref)
	return u.ID, err
}

type findIn struct {
	Query    string   `json:"query,omitempty" jsonschema:"words in the title"`
	Types    []string `json:"types,omitempty" jsonschema:"item types; omit for all"`
	Library  string   `json:"library,omitempty" jsonschema:"library name or id"`
	Genres   []string `json:"genres,omitempty" jsonschema:"match any of these genres"`
	Years    []int    `json:"years,omitempty" jsonschema:"production years"`
	Person   string   `json:"person,omitempty" jsonschema:"actor, director or other person, by name"`
	Played   *bool    `json:"played,omitempty" jsonschema:"true for only watched, false for only unwatched, by the user"`
	Favorite *bool    `json:"favorite,omitempty" jsonschema:"true for only the user's favorites"`
	Sort     string   `json:"sort,omitempty" jsonschema:"defaults to name; not with query, which is always ordered by relevance"`
	Order    string   `json:"order,omitempty" jsonschema:"defaults to the natural order of sort: newest, highest or most first"`
	Limit    int      `json:"limit,omitempty" jsonschema:"page size, default 20, at most 100"`
	Offset   int      `json:"offset,omitempty"`
	userIn
}

type itemsOut struct {
	Items []item `json:"items"`
	Total int    `json:"total"`
}

func (t *Tools) findItems(ctx context.Context, in findIn) (itemsOut, error) {
	needUser := in.Played != nil || in.Favorite != nil
	var userID string
	var err error
	if needUser {
		var u jellyfin.User
		u, err = t.user(ctx, in.User)
		userID = u.ID
	} else {
		userID, err = t.optionalUser(ctx, in.User)
	}
	if err != nil {
		return itemsOut{}, err
	}

	q := jellyfin.ItemsQuery{
		UserID:     userID,
		SearchTerm: strings.TrimSpace(in.Query),
		Types:      in.Types,
		Genres:     in.Genres,
		Years:      in.Years,
		Person:     strings.TrimSpace(in.Person),
		IsPlayed:   in.Played,
		IsFavorite: in.Favorite,
		Fields:     []string{"Genres", "DateCreated", "ChildCount"},
		StartIndex: max(in.Offset, 0),
		Limit:      clampLimit(in.Limit, 20, 100),
		Recursive:  true,
	}
	if len(q.Types) == 0 {
		q.Types = searchTypes
	}
	if in.Library != "" {
		lib, err := t.library(ctx, in.Library)
		if err != nil {
			return itemsOut{}, err
		}
		q.ParentID = lib.ItemID
	}
	// Jellyfin orders text searches by relevance and name before any sort.
	if q.SearchTerm != "" && (in.Sort != "" || in.Order != "") {
		return itemsOut{}, fmt.Errorf("sort and order don't apply with query, whose results come by relevance; " +
			"filter without query to sort, or sort the results yourself")
	}
	if q.SearchTerm == "" {
		sort := in.Sort
		if sort == "" {
			sort = "name"
		}
		s, ok := sorts[sort]
		if !ok {
			return itemsOut{}, fmt.Errorf("unknown sort %q", sort)
		}
		q.SortBy = strings.Split(s.by, ",")
		q.SortOrder = s.order
		switch in.Order {
		case "asc":
			q.SortOrder = "Ascending"
		case "desc":
			q.SortOrder = "Descending"
		}
	}

	r, err := t.c.Items(ctx, q)
	if err != nil {
		return itemsOut{}, err
	}
	return itemsOut{Items: toItems(r.Items), Total: r.TotalRecordCount}, nil
}

type itemIn struct {
	Item string `json:"item" jsonschema:"item id, or name with an optional (year)"`
	userIn
}

type castMember struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

type itemDetails struct {
	item
	OriginalTitle string            `json:"original_title,omitempty"`
	Overview      string            `json:"overview,omitempty"`
	Tagline       string            `json:"tagline,omitempty"`
	Status        string            `json:"status,omitempty" jsonschema:"for series: Continuing or Ended"`
	EndDate       string            `json:"end_date,omitempty"`
	Studios       []string          `json:"studios,omitempty"`
	Cast          []castMember      `json:"cast,omitempty" jsonschema:"top billed actors with their roles"`
	Crew          []castMember      `json:"crew,omitempty" jsonschema:"directors, writers and others, with their job as role"`
	Tags          []string          `json:"tags,omitempty"`
	ExternalIDs   map[string]string `json:"external_ids,omitempty" jsonschema:"ids at IMDb, TMDB, TVDB, MusicBrainz and so on"`
	SeriesID      string            `json:"series_id,omitempty"`
	Path          string            `json:"path,omitempty"`
	Video         []string          `json:"video,omitempty"`
	Audio         []string          `json:"audio,omitempty"`
	Subtitles     []string          `json:"subtitles,omitempty"`
}

const maxCast = 15

func (t *Tools) getItem(ctx context.Context, in itemIn) (itemDetails, error) {
	userID, err := t.optionalUser(ctx, in.User)
	if err != nil {
		return itemDetails{}, err
	}
	ref, err := t.item(ctx, in.Item, userID)
	if err != nil {
		return itemDetails{}, err
	}
	it, err := t.c.Item(ctx, ref.ID, userID)
	if err != nil {
		return itemDetails{}, err
	}

	out := itemDetails{
		item:          toItem(it),
		OriginalTitle: it.OriginalTitle,
		Overview:      it.Overview,
		Status:        it.Status,
		EndDate:       date(it.EndDate),
		Tags:          it.Tags,
		ExternalIDs:   it.ProviderIDs,
		SeriesID:      it.SeriesID,
		Path:          it.Path,
	}
	if out.OriginalTitle == it.Name {
		out.OriginalTitle = ""
	}
	if len(it.Taglines) > 0 {
		out.Tagline = it.Taglines[0]
	}
	for _, s := range it.Studios {
		out.Studios = append(out.Studios, s.Name)
	}
	for _, p := range it.People {
		switch {
		case p.Type == "Actor" || p.Type == "GuestStar":
			if len(out.Cast) < maxCast {
				out.Cast = append(out.Cast, castMember{Name: p.Name, Role: p.Role})
			}
		case len(out.Crew) < maxCast:
			out.Crew = append(out.Crew, castMember{Name: p.Name, Role: p.Type})
		}
	}
	for _, s := range it.MediaStreams {
		title := s.DisplayTitle
		if title == "" {
			title = strings.TrimSpace(s.Language + " " + s.Codec)
		}
		switch s.Type {
		case "Video":
			out.Video = append(out.Video, title)
		case "Audio":
			out.Audio = append(out.Audio, title)
		case "Subtitle":
			out.Subtitles = append(out.Subtitles, title)
		}
	}
	return out, nil
}

type librariesIn struct{}

type library struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type,omitempty" jsonschema:"movies, tvshows, music, books, homevideos, boxsets, playlists or mixed"`
	Folders []string `json:"folders,omitempty"`
}

type counts struct {
	Movies      int `json:"movies"`
	Series      int `json:"series"`
	Episodes    int `json:"episodes"`
	Artists     int `json:"artists"`
	Albums      int `json:"albums"`
	Songs       int `json:"songs"`
	MusicVideos int `json:"music_videos"`
	Books       int `json:"books"`
	Collections int `json:"collections"`
}

type librariesOut struct {
	Libraries []library `json:"libraries"`
	Counts    counts    `json:"counts"`
}

func (t *Tools) libraries(ctx context.Context, _ librariesIn) (librariesOut, error) {
	fs, err := t.c.VirtualFolders(ctx)
	if err != nil {
		return librariesOut{}, err
	}
	n, err := t.c.ItemCounts(ctx)
	if err != nil {
		return librariesOut{}, err
	}
	out := librariesOut{
		Libraries: make([]library, len(fs)),
		Counts: counts{
			Movies:      n.MovieCount,
			Series:      n.SeriesCount,
			Episodes:    n.EpisodeCount,
			Artists:     n.ArtistCount,
			Albums:      n.AlbumCount,
			Songs:       n.SongCount,
			MusicVideos: n.MusicVideoCount,
			Books:       n.BookCount,
			Collections: n.BoxSetCount,
		},
	}
	for i, f := range fs {
		typ := f.CollectionType
		if typ == "" {
			typ = "mixed"
		}
		out.Libraries[i] = library{ID: f.ItemID, Name: f.Name, Type: typ, Folders: f.Locations}
	}
	return out, nil
}

type latestIn struct {
	Library string   `json:"library,omitempty" jsonschema:"library name or id; omit for all"`
	Types   []string `json:"types,omitempty"`
	Limit   int      `json:"limit,omitempty" jsonschema:"default 16, at most 100"`
	userIn
}

type listOut struct {
	Items []item `json:"items"`
}

func (t *Tools) latest(ctx context.Context, in latestIn) (listOut, error) {
	u, err := t.user(ctx, in.User)
	if err != nil {
		return listOut{}, err
	}
	var parent string
	if in.Library != "" {
		lib, err := t.library(ctx, in.Library)
		if err != nil {
			return listOut{}, err
		}
		parent = lib.ItemID
	}
	its, err := t.c.Latest(ctx, u.ID, parent, in.Types, clampLimit(in.Limit, 16, 100))
	if err != nil {
		return listOut{}, err
	}
	return listOut{Items: toItems(its)}, nil
}

type continueIn struct {
	Types []string `json:"types,omitempty"`
	Limit int      `json:"limit,omitempty" jsonschema:"default 12, at most 100"`
	userIn
}

func (t *Tools) continueWatching(ctx context.Context, in continueIn) (itemsOut, error) {
	u, err := t.user(ctx, in.User)
	if err != nil {
		return itemsOut{}, err
	}
	r, err := t.c.Resume(ctx, u.ID, in.Types, clampLimit(in.Limit, 12, 100))
	if err != nil {
		return itemsOut{}, err
	}
	return itemsOut{Items: toItems(r.Items), Total: r.TotalRecordCount}, nil
}

type nextUpIn struct {
	Series string `json:"series,omitempty" jsonschema:"series id or name; omit for every series in progress"`
	Limit  int    `json:"limit,omitempty" jsonschema:"default 12, at most 100"`
	userIn
}

func (t *Tools) nextUp(ctx context.Context, in nextUpIn) (itemsOut, error) {
	u, err := t.user(ctx, in.User)
	if err != nil {
		return itemsOut{}, err
	}
	var seriesID string
	if in.Series != "" {
		s, err := t.item(ctx, in.Series, u.ID, "Series")
		if err != nil {
			return itemsOut{}, err
		}
		seriesID = s.ID
	}
	r, err := t.c.NextUp(ctx, u.ID, seriesID, clampLimit(in.Limit, 12, 100))
	if err != nil {
		return itemsOut{}, err
	}
	return itemsOut{Items: toItems(r.Items), Total: r.TotalRecordCount}, nil
}

type episodesIn struct {
	Series       string `json:"series" jsonschema:"series id or name"`
	Season       *int   `json:"season,omitempty" jsonschema:"season number; 0 is specials; omit for all seasons"`
	UnplayedOnly bool   `json:"unplayed_only,omitempty" jsonschema:"only episodes the user has not watched"`
	Limit        int    `json:"limit,omitempty" jsonschema:"page size, default 100, at most 200"`
	Offset       int    `json:"offset,omitempty"`
	userIn
}

type episodesOut struct {
	Series     string `json:"series"`
	SeriesID   string `json:"series_id"`
	Episodes   []item `json:"episodes"`
	Total      int    `json:"total" jsonschema:"matching episodes, on every page"`
	Unplayed   int    `json:"unplayed" jsonschema:"unwatched episodes among them"`
	NextOffset int    `json:"next_offset,omitempty" jsonschema:"pass as offset for the next page; absent on the last page"`
}

func (t *Tools) episodes(ctx context.Context, in episodesIn) (episodesOut, error) {
	userID, err := t.optionalUser(ctx, in.User)
	if err != nil {
		return episodesOut{}, err
	}
	if in.UnplayedOnly && userID == "" {
		u, err := t.user(ctx, in.User)
		if err != nil {
			return episodesOut{}, err
		}
		userID = u.ID
	}
	s, err := t.item(ctx, in.Series, userID, "Series")
	if err != nil {
		return episodesOut{}, err
	}
	if s.Type != "Series" {
		return episodesOut{}, fmt.Errorf("%s is a %s, not a series", s.Name, s.Type)
	}
	r, err := t.c.Episodes(ctx, s.ID, userID, in.Season)
	if err != nil {
		return episodesOut{}, err
	}
	out := episodesOut{Series: s.Name, SeriesID: s.ID, Episodes: []item{}}
	offset, limit := max(in.Offset, 0), clampLimit(in.Limit, 100, 200)
	for _, e := range r.Items {
		if e.UserData != nil && !e.UserData.Played {
			out.Unplayed++
		} else if in.UnplayedOnly {
			continue
		}
		if out.Total >= offset && len(out.Episodes) < limit {
			out.Episodes = append(out.Episodes, toItem(e))
		}
		out.Total++
	}
	if next := offset + len(out.Episodes); next < out.Total {
		out.NextOffset = next
	}
	return out, nil
}

type similarIn struct {
	Item  string `json:"item" jsonschema:"item id, or name with an optional (year)"`
	Limit int    `json:"limit,omitempty" jsonschema:"default 10, at most 50"`
	userIn
}

func (t *Tools) similar(ctx context.Context, in similarIn) (listOut, error) {
	userID, err := t.optionalUser(ctx, in.User)
	if err != nil {
		return listOut{}, err
	}
	it, err := t.item(ctx, in.Item, userID)
	if err != nil {
		return listOut{}, err
	}
	limit := clampLimit(in.Limit, 10, 50)
	r, err := t.c.Similar(ctx, it.ID, userID, limit)
	if err != nil {
		return listOut{}, err
	}
	// Some Jellyfin versions return more than limit.
	return listOut{Items: toItems(r.Items[:min(len(r.Items), limit)])}, nil
}
