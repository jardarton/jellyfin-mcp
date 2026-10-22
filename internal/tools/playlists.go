package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jardarton/jellyfin-mcp/internal/jellyfin"
)

func (t *Tools) registerPlaylists(server *mcp.Server) {
	add(t, server, tool{
		name:        "playlists",
		description: "List the playlists the user can see (their own and shared ones), or, given a playlist, its items in order.",
		readOnly:    true,
	}, t.playlists)
	add(t, server, tool{
		name:        "create_playlist",
		description: "Create a playlist owned by the user, optionally with items. Returns the new playlist.",
	}, t.createPlaylist)
	add(t, server, tool{
		name:        "add_to_playlist",
		description: "Append items to the end of one of the user's playlists.",
	}, t.addToPlaylist)
	add(t, server, tool{
		name:        "remove_from_playlist",
		description: "Remove items from one of the user's playlists. Every entry of a given item is removed.",
		destructive: true,
	}, t.removeFromPlaylist)
}

type playlistsIn struct {
	Playlist string `json:"playlist,omitempty" jsonschema:"playlist id or name; omit to list playlists"`
	userIn
}

type playlistsOut struct {
	Playlists []item `json:"playlists,omitzero"`
	Playlist  *item  `json:"playlist,omitzero"`
	Items     []item `json:"items,omitzero"`
}

func (t *Tools) playlists(ctx context.Context, in playlistsIn) (playlistsOut, error) {
	u, err := t.user(ctx, in.User)
	if err != nil {
		return playlistsOut{}, err
	}
	if in.Playlist == "" {
		r, err := t.c.Items(ctx, jellyfin.ItemsQuery{
			UserID:    u.ID,
			Types:     []string{"Playlist"},
			Recursive: true,
			SortBy:    []string{"SortName"},
			Fields:    []string{"DateCreated", "ChildCount"},
		})
		if err != nil {
			return playlistsOut{}, err
		}
		return playlistsOut{Playlists: toItems(r.Items)}, nil
	}

	p, err := t.playlist(ctx, in.Playlist, u.ID)
	if err != nil {
		return playlistsOut{}, err
	}
	r, err := t.c.PlaylistItems(ctx, p.ID, u.ID)
	if err != nil {
		return playlistsOut{}, err
	}
	pi := toItem(p)
	return playlistsOut{Playlist: &pi, Items: toItems(r.Items)}, nil
}

func (t *Tools) playlist(ctx context.Context, ref, userID string) (jellyfin.Item, error) {
	p, err := t.item(ctx, ref, userID, "Playlist")
	if err != nil {
		return p, err
	}
	if p.Type != "Playlist" {
		return p, fmt.Errorf("%s is a %s, not a playlist", p.Name, p.Type)
	}
	return p, nil
}

type createPlaylistIn struct {
	Name   string   `json:"name"`
	Items  []string `json:"items,omitempty" jsonschema:"item ids, or names with an optional (year)"`
	Public bool     `json:"public,omitempty" jsonschema:"let every user see the playlist"`
	userIn
}

type playlistOut struct {
	User     string `json:"user"`
	Playlist item   `json:"playlist"`
	Items    []item `json:"items" jsonschema:"the playlist's items after the change"`
}

func (t *Tools) createPlaylist(ctx context.Context, in createPlaylistIn) (playlistOut, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return playlistOut{}, fmt.Errorf("name is required")
	}
	u, err := t.user(ctx, in.User)
	if err != nil {
		return playlistOut{}, err
	}
	var its []jellyfin.Item
	if len(in.Items) > 0 {
		if its, err = t.items(ctx, in.Items, u.ID); err != nil {
			return playlistOut{}, err
		}
	}
	id, err := t.c.CreatePlaylist(ctx, u.ID, name, ids(its), in.Public)
	if err != nil {
		return playlistOut{}, err
	}
	return t.playlistAfter(ctx, u, id)
}

type playlistItemsIn struct {
	Playlist string   `json:"playlist" jsonschema:"playlist id or name"`
	Items    []string `json:"items" jsonschema:"item ids, or names with an optional (year)"`
	userIn
}

func (t *Tools) addToPlaylist(ctx context.Context, in playlistItemsIn) (playlistOut, error) {
	u, err := t.user(ctx, in.User)
	if err != nil {
		return playlistOut{}, err
	}
	p, err := t.playlist(ctx, in.Playlist, u.ID)
	if err != nil {
		return playlistOut{}, err
	}
	its, err := t.items(ctx, in.Items, u.ID)
	if err != nil {
		return playlistOut{}, err
	}
	if err := t.c.AddToPlaylist(ctx, p.ID, u.ID, ids(its)); err != nil {
		return playlistOut{}, err
	}
	return t.playlistAfter(ctx, u, p.ID)
}

func (t *Tools) removeFromPlaylist(ctx context.Context, in playlistItemsIn) (playlistOut, error) {
	if len(in.Items) == 0 {
		return playlistOut{}, fmt.Errorf("items is required")
	}
	u, err := t.user(ctx, in.User)
	if err != nil {
		return playlistOut{}, err
	}
	p, err := t.playlist(ctx, in.Playlist, u.ID)
	if err != nil {
		return playlistOut{}, err
	}
	r, err := t.c.PlaylistItems(ctx, p.ID, u.ID)
	if err != nil {
		return playlistOut{}, err
	}

	// Match against the playlist's own entries so names only need to be
	// unique within the playlist.
	var entries []string
	for _, ref := range in.Items {
		ref = strings.TrimSpace(ref)
		found := false
		for _, e := range r.Items {
			if sameID(e.ID, ref) || strings.EqualFold(e.Name, ref) {
				entries = append(entries, e.PlaylistItemID)
				found = true
			}
		}
		if !found {
			return playlistOut{}, fmt.Errorf("%q is not in playlist %s", ref, p.Name)
		}
	}
	if err := t.c.RemoveFromPlaylist(ctx, p.ID, entries); err != nil {
		return playlistOut{}, err
	}
	return t.playlistAfter(ctx, u, p.ID)
}

func (t *Tools) playlistAfter(ctx context.Context, u jellyfin.User, id string) (playlistOut, error) {
	p, err := t.c.Item(ctx, id, u.ID)
	if err != nil {
		return playlistOut{}, err
	}
	r, err := t.c.PlaylistItems(ctx, id, u.ID)
	if err != nil {
		return playlistOut{}, err
	}
	return playlistOut{User: u.Name, Playlist: toItem(p), Items: toItems(r.Items)}, nil
}
