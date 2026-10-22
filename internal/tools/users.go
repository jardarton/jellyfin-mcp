package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (t *Tools) registerUsers(server *mcp.Server) {
	add(t, server, tool{
		name:        "users",
		description: "The server's users, with whether they are administrators or disabled, and when they were last active.",
		readOnly:    true,
	}, t.users)
	add(t, server, tool{
		name: "mark_played",
		description: "Mark items watched for the user, as if played to the end: the play count becomes at least one " +
			"and where playback stopped is forgotten. Marking a series, season or album marks everything in it.",
		destructive: true,
	}, t.markPlayed)
	add(t, server, tool{
		name: "mark_unplayed",
		description: "Mark items unwatched for the user. This erases their play count, last played date and where playback stopped. " +
			"Marking a series, season or album marks everything in it.",
		destructive: true,
	}, t.markUnplayed)
	add(t, server, tool{
		name:        "set_favorite",
		description: "Add items to or remove them from the user's favorites.",
	}, t.setFavorite)
}

type usersIn struct{}

type userInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Admin        bool   `json:"admin,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
	LastActivity string `json:"last_activity,omitempty"`
	LastLogin    string `json:"last_login,omitempty"`
}

type usersOut struct {
	Users       []userInfo `json:"users"`
	DefaultUser string     `json:"default_user,omitempty" jsonschema:"the user tools act for when no user is given"`
}

func (t *Tools) users(ctx context.Context, _ usersIn) (usersOut, error) {
	us, err := t.c.Users(ctx)
	if err != nil {
		return usersOut{}, err
	}
	out := usersOut{Users: make([]userInfo, len(us))}
	for i, u := range us {
		out.Users[i] = userInfo{
			ID:           u.ID,
			Name:         u.Name,
			Admin:        u.Policy.IsAdministrator,
			Disabled:     u.Policy.IsDisabled,
			LastActivity: timestamp(u.LastActivityDate),
			LastLogin:    timestamp(u.LastLoginDate),
		}
	}
	if d, err := t.user(ctx, ""); err == nil {
		out.DefaultUser = d.Name
	}
	return out, nil
}

type itemsIn struct {
	Items []string `json:"items" jsonschema:"item ids, or names with an optional (year)"`
	userIn
}

type setFavoriteIn struct {
	Items    []string `json:"items" jsonschema:"item ids, or names with an optional (year)"`
	Favorite bool     `json:"favorite" jsonschema:"true to add to favorites, false to remove"`
	userIn
}

type changedOut struct {
	User  string `json:"user"`
	Items []item `json:"items" jsonschema:"the items with their new state"`
}

func (t *Tools) markPlayed(ctx context.Context, in itemsIn) (changedOut, error) {
	return t.setPlayed(ctx, in, true)
}

func (t *Tools) markUnplayed(ctx context.Context, in itemsIn) (changedOut, error) {
	return t.setPlayed(ctx, in, false)
}

func (t *Tools) setPlayed(ctx context.Context, in itemsIn, played bool) (changedOut, error) {
	u, err := t.user(ctx, in.User)
	if err != nil {
		return changedOut{}, err
	}
	its, err := t.items(ctx, in.Items, u.ID)
	if err != nil {
		return changedOut{}, err
	}
	out := changedOut{User: u.Name, Items: make([]item, len(its))}
	for i, it := range its {
		d, err := t.c.SetPlayed(ctx, u.ID, it.ID, played)
		if err != nil {
			return changedOut{}, err
		}
		it.UserData = &d
		out.Items[i] = toItem(it)
	}
	return out, nil
}

func (t *Tools) setFavorite(ctx context.Context, in setFavoriteIn) (changedOut, error) {
	u, err := t.user(ctx, in.User)
	if err != nil {
		return changedOut{}, err
	}
	its, err := t.items(ctx, in.Items, u.ID)
	if err != nil {
		return changedOut{}, err
	}
	out := changedOut{User: u.Name, Items: make([]item, len(its))}
	for i, it := range its {
		d, err := t.c.SetFavorite(ctx, u.ID, it.ID, in.Favorite)
		if err != nil {
			return changedOut{}, err
		}
		it.UserData = &d
		out.Items[i] = toItem(it)
	}
	return out, nil
}
