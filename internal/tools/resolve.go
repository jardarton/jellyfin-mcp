package tools

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jardarton/jellyfin-mcp/internal/jellyfin"
)

var (
	idPattern   = regexp.MustCompile(`^[0-9a-fA-F]{32}$|^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$`)
	yearPattern = regexp.MustCompile(`^(.*\S)\s*\((\d{4})\)$`)
)

// searchTypes are the item types a name lookup or search covers by default.
var searchTypes = []string{
	"Movie", "Series", "Season", "Episode", "BoxSet", "MusicAlbum", "MusicArtist", "Audio",
	"MusicVideo", "AudioBook", "Book", "Video", "Playlist",
}

func isID(s string) bool { return idPattern.MatchString(s) }

func sameID(a, b string) bool {
	return strings.EqualFold(strings.ReplaceAll(a, "-", ""), strings.ReplaceAll(b, "-", ""))
}

// user resolves a user name or id, falling back to the configured default
// and then to the only enabled user.
func (t *Tools) user(ctx context.Context, ref string) (jellyfin.User, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = t.opts.DefaultUser
	}
	users, err := t.c.Users(ctx)
	if err != nil {
		return jellyfin.User{}, err
	}
	var enabled []jellyfin.User
	for _, u := range users {
		if ref != "" && (strings.EqualFold(u.Name, ref) || sameID(u.ID, ref)) {
			return u, nil
		}
		if !u.Policy.IsDisabled {
			enabled = append(enabled, u)
		}
	}
	if ref == "" && len(enabled) == 1 {
		return enabled[0], nil
	}
	names := make([]string, len(enabled))
	for i, u := range enabled {
		names[i] = u.Name
	}
	if ref == "" {
		return jellyfin.User{}, fmt.Errorf("this needs a Jellyfin user; pass user, one of %s", strings.Join(names, ", "))
	}
	return jellyfin.User{}, fmt.Errorf("no user %q; users are %s", ref, strings.Join(names, ", "))
}

// item resolves an item id, or a name with an optional "(year)", among types
// (searchTypes when empty). userID, when set, adds that user's state.
func (t *Tools) item(ctx context.Context, ref, userID string, types ...string) (jellyfin.Item, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return jellyfin.Item{}, fmt.Errorf("item is required")
	}
	if isID(ref) {
		it, err := t.c.Item(ctx, ref, userID)
		if jellyfin.IsNotFound(err) {
			return it, fmt.Errorf("no item with id %s", ref)
		}
		return it, err
	}

	name, year := ref, 0
	if m := yearPattern.FindStringSubmatch(ref); m != nil {
		name = m[1]
		year, _ = strconv.Atoi(m[2])
	}
	kinds := types
	if len(kinds) == 0 {
		kinds = searchTypes
	}
	q := jellyfin.ItemsQuery{
		UserID:     userID,
		SearchTerm: name,
		Types:      kinds,
		Recursive:  true,
		Limit:      25,
	}
	if year != 0 {
		q.Years = []int{year}
	}
	r, err := t.c.Items(ctx, q)
	if err != nil {
		return jellyfin.Item{}, err
	}

	var exact []jellyfin.Item
	for _, it := range r.Items {
		if strings.EqualFold(it.Name, name) || strings.EqualFold(it.OriginalTitle, name) {
			exact = append(exact, it)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) == 0 && len(r.Items) == 1:
		return r.Items[0], nil
	case len(exact) > 1:
		return jellyfin.Item{}, ambiguous(ref, exact)
	case len(r.Items) > 1:
		return jellyfin.Item{}, ambiguous(ref, r.Items)
	}
	kind := "item"
	if len(types) == 1 {
		kind = strings.ToLower(types[0])
	}
	return jellyfin.Item{}, fmt.Errorf("no %s named %q; try find_items with a shorter query", kind, ref)
}

func ambiguous(ref string, its []jellyfin.Item) error {
	const max = 8
	var names []string
	for i, it := range its {
		if i == max {
			names = append(names, fmt.Sprintf("and %d more", len(its)-max))
			break
		}
		names = append(names, describe(it))
	}
	return fmt.Errorf("several items match %q, pass the id of one: %s", ref, strings.Join(names, "; "))
}

// describe names an item for a person: "Pilot (Show S01E01, Episode) id ...".
func describe(it jellyfin.Item) string {
	var parts []string
	if it.SeriesName != "" && it.Type == "Episode" && it.ParentIndexNumber != nil && it.IndexNumber != nil {
		parts = append(parts, fmt.Sprintf("%s S%02dE%02d", it.SeriesName, *it.ParentIndexNumber, *it.IndexNumber))
	} else if it.SeriesName != "" {
		parts = append(parts, it.SeriesName)
	}
	if it.ProductionYear != 0 {
		parts = append(parts, strconv.Itoa(it.ProductionYear))
	}
	if a := artist(it); a != "" {
		parts = append(parts, a)
	}
	parts = append(parts, it.Type)
	return fmt.Sprintf("%s (%s) id %s", it.Name, strings.Join(parts, ", "), it.ID)
}

// items resolves several item references.
func (t *Tools) items(ctx context.Context, refs []string, userID string) ([]jellyfin.Item, error) {
	if len(refs) == 0 {
		return nil, fmt.Errorf("items is required")
	}
	out := make([]jellyfin.Item, 0, len(refs))
	for _, ref := range refs {
		it, err := t.item(ctx, ref, userID)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

func ids(its []jellyfin.Item) []string {
	out := make([]string, len(its))
	for i, it := range its {
		out[i] = it.ID
	}
	return out
}

// library resolves a library name or id.
func (t *Tools) library(ctx context.Context, ref string) (jellyfin.VirtualFolder, error) {
	fs, err := t.c.VirtualFolders(ctx)
	if err != nil {
		return jellyfin.VirtualFolder{}, err
	}
	names := make([]string, len(fs))
	for i, f := range fs {
		if strings.EqualFold(f.Name, strings.TrimSpace(ref)) || sameID(f.ItemID, ref) {
			return f, nil
		}
		names[i] = f.Name
	}
	return jellyfin.VirtualFolder{}, fmt.Errorf("no library %q; libraries are %s", ref, strings.Join(names, ", "))
}

// session resolves a session id, device name or client name to one session.
func (t *Tools) session(ctx context.Context, ref string) (jellyfin.Session, error) {
	ref = strings.TrimSpace(ref)
	ss, err := t.c.Sessions(ctx, 0)
	if err != nil {
		return jellyfin.Session{}, err
	}
	var matches []jellyfin.Session
	for _, s := range ss {
		if s.ID == ref {
			return s, nil
		}
		if strings.EqualFold(s.DeviceName, ref) || strings.EqualFold(s.Client, ref) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, s := range matches {
			names[i] = fmt.Sprintf("%s on %s, user %s, id %s", s.Client, s.DeviceName, orNone(s.UserName), s.ID)
		}
		return jellyfin.Session{}, fmt.Errorf("several sessions match %q, pass the id of one: %s", ref, strings.Join(names, "; "))
	}
	return jellyfin.Session{}, fmt.Errorf("no session %q; list them with sessions", ref)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
