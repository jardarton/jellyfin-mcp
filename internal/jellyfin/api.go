package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// usersTTL is how long Users caches the user list.
const usersTTL = time.Minute

// ItemsQuery filters GET /Items. Zero fields are left out.
type ItemsQuery struct {
	UserID     string
	SearchTerm string
	ParentID   string
	IDs        []string
	Types      []string
	Genres     []string
	Years      []int
	Person     string
	Studios    []string
	IsPlayed   *bool
	IsFavorite *bool
	SortBy     []string
	SortOrder  string
	Fields     []string
	StartIndex int
	Limit      int
	Recursive  bool
}

func (q ItemsQuery) values() url.Values {
	v := url.Values{}
	set(v, "userId", q.UserID)
	set(v, "searchTerm", q.SearchTerm)
	set(v, "parentId", q.ParentID)
	set(v, "ids", strings.Join(q.IDs, ","))
	set(v, "includeItemTypes", strings.Join(q.Types, ","))
	set(v, "genres", strings.Join(q.Genres, "|"))
	set(v, "studios", strings.Join(q.Studios, "|"))
	set(v, "person", q.Person)
	set(v, "sortBy", strings.Join(q.SortBy, ","))
	set(v, "sortOrder", q.SortOrder)
	set(v, "fields", strings.Join(q.Fields, ","))
	if len(q.Years) > 0 {
		ys := make([]string, len(q.Years))
		for i, y := range q.Years {
			ys[i] = strconv.Itoa(y)
		}
		v.Set("years", strings.Join(ys, ","))
	}
	if q.IsPlayed != nil {
		v.Set("isPlayed", strconv.FormatBool(*q.IsPlayed))
	}
	if q.IsFavorite != nil {
		v.Set("isFavorite", strconv.FormatBool(*q.IsFavorite))
	}
	if q.StartIndex > 0 {
		v.Set("startIndex", strconv.Itoa(q.StartIndex))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Recursive {
		v.Set("recursive", "true")
	}
	v.Set("enableImages", "false")
	return v
}

func set(v url.Values, key, value string) {
	if value != "" {
		v.Set(key, value)
	}
}

// Items lists items matching q.
func (c *Client) Items(ctx context.Context, q ItemsQuery) (ItemsResult, error) {
	var r ItemsResult
	err := c.get(ctx, "Items", q.values(), &r)
	return r, err
}

// itemFields are the fields Item asks for.
var itemFields = []string{
	"OriginalTitle", "Overview", "Taglines", "Genres", "Tags", "Studios", "People", "ProviderIds",
	"Path", "MediaStreams", "DateCreated", "ChildCount", "ParentId",
}

// ErrNotFound is returned by Item for an id the server doesn't have.
var ErrNotFound = errors.New("not found")

// Item returns one item with its details, and userID's state when userID is
// set. It goes through /Items rather than /Items/{id}, which needs a user
// when authenticated with an API key.
func (c *Client) Item(ctx context.Context, id, userID string) (Item, error) {
	r, err := c.Items(ctx, ItemsQuery{UserID: userID, IDs: []string{id}, Fields: itemFields})
	if err != nil {
		return Item{}, err
	}
	if len(r.Items) == 0 {
		return Item{}, fmt.Errorf("item %s: %w", id, ErrNotFound)
	}
	return r.Items[0], nil
}

// Latest returns recently added items for userID, grouped like the home screen.
func (c *Client) Latest(ctx context.Context, userID, parentID string, types []string, limit int) ([]Item, error) {
	v := url.Values{}
	set(v, "userId", userID)
	set(v, "parentId", parentID)
	set(v, "includeItemTypes", strings.Join(types, ","))
	v.Set("limit", strconv.Itoa(limit))
	v.Set("fields", "DateCreated")
	v.Set("enableImages", "false")
	var items []Item
	err := c.get(ctx, "Items/Latest", v, &items)
	return items, err
}

// Resume returns userID's partly played items.
func (c *Client) Resume(ctx context.Context, userID string, types []string, limit int) (ItemsResult, error) {
	v := url.Values{}
	set(v, "userId", userID)
	set(v, "includeItemTypes", strings.Join(types, ","))
	v.Set("limit", strconv.Itoa(limit))
	v.Set("enableImages", "false")
	var r ItemsResult
	err := c.get(ctx, "UserItems/Resume", v, &r)
	return r, err
}

// NextUp returns the next episodes to watch for userID, optionally for one series.
func (c *Client) NextUp(ctx context.Context, userID, seriesID string, limit int) (ItemsResult, error) {
	v := url.Values{}
	set(v, "userId", userID)
	set(v, "seriesId", seriesID)
	v.Set("limit", strconv.Itoa(limit))
	v.Set("fields", "PremiereDate")
	v.Set("enableImages", "false")
	var r ItemsResult
	err := c.get(ctx, "Shows/NextUp", v, &r)
	return r, err
}

// Episodes returns a series' episodes, optionally for one season number.
func (c *Client) Episodes(ctx context.Context, seriesID, userID string, season *int) (ItemsResult, error) {
	v := url.Values{}
	set(v, "userId", userID)
	if season != nil {
		v.Set("season", strconv.Itoa(*season))
	}
	v.Set("fields", "PremiereDate")
	v.Set("enableImages", "false")
	var r ItemsResult
	err := c.get(ctx, "Shows/"+url.PathEscape(seriesID)+"/Episodes", v, &r)
	return r, err
}

// Similar returns items similar to id.
func (c *Client) Similar(ctx context.Context, id, userID string, limit int) (ItemsResult, error) {
	v := url.Values{}
	set(v, "userId", userID)
	v.Set("limit", strconv.Itoa(limit))
	var r ItemsResult
	err := c.get(ctx, "Items/"+url.PathEscape(id)+"/Similar", v, &r)
	return r, err
}

// VirtualFolders returns the configured libraries.
func (c *Client) VirtualFolders(ctx context.Context) ([]VirtualFolder, error) {
	var fs []VirtualFolder
	err := c.get(ctx, "Library/VirtualFolders", nil, &fs)
	return fs, err
}

// ItemCounts returns the server-wide item counts.
func (c *Client) ItemCounts(ctx context.Context) (ItemCounts, error) {
	var n ItemCounts
	err := c.get(ctx, "Items/Counts", nil, &n)
	return n, err
}

// Users returns all users. The list is cached briefly.
func (c *Client) Users(ctx context.Context) ([]User, error) {
	c.mu.Lock()
	if c.users != nil && time.Since(c.usersAt) < usersTTL {
		us := c.users
		c.mu.Unlock()
		return us, nil
	}
	c.mu.Unlock()

	var us []User
	if err := c.get(ctx, "Users", nil, &us); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.users, c.usersAt = us, time.Now()
	c.mu.Unlock()
	return us, nil
}

// Sessions returns sessions active within the given duration.
func (c *Client) Sessions(ctx context.Context, activeWithin time.Duration) ([]Session, error) {
	v := url.Values{}
	if activeWithin > 0 {
		v.Set("activeWithinSeconds", strconv.Itoa(int(activeWithin.Seconds())))
	}
	var ss []Session
	err := c.get(ctx, "Sessions", v, &ss)
	return ss, err
}

// SetPlayed marks id played or unplayed for userID.
func (c *Client) SetPlayed(ctx context.Context, userID, id string, played bool) (UserData, error) {
	v := url.Values{"userId": {userID}}
	path := "UserPlayedItems/" + url.PathEscape(id)
	var d UserData
	if played {
		err := c.post(ctx, path, v, nil, &d)
		return d, err
	}
	err := c.do(ctx, http.MethodDelete, path, v, nil, &d)
	return d, err
}

// SetFavorite adds id to or removes it from userID's favorites.
func (c *Client) SetFavorite(ctx context.Context, userID, id string, favorite bool) (UserData, error) {
	v := url.Values{"userId": {userID}}
	path := "UserFavoriteItems/" + url.PathEscape(id)
	var d UserData
	if favorite {
		err := c.post(ctx, path, v, nil, &d)
		return d, err
	}
	err := c.do(ctx, http.MethodDelete, path, v, nil, &d)
	return d, err
}

// CreatePlaylist creates a playlist owned by userID and returns its id.
func (c *Client) CreatePlaylist(ctx context.Context, userID, name string, ids []string, public bool) (string, error) {
	body := map[string]any{"Name": name, "Ids": ids, "UserId": userID, "IsPublic": public}
	var r struct {
		ID string `json:"Id"`
	}
	err := c.post(ctx, "Playlists", nil, body, &r)
	return r.ID, err
}

// AddToPlaylist appends ids to a playlist.
func (c *Client) AddToPlaylist(ctx context.Context, playlistID, userID string, ids []string) error {
	v := url.Values{"ids": {strings.Join(ids, ",")}}
	set(v, "userId", userID)
	return c.post(ctx, "Playlists/"+url.PathEscape(playlistID)+"/Items", v, nil, nil)
}

// PlaylistItems returns a playlist's entries in order.
func (c *Client) PlaylistItems(ctx context.Context, playlistID, userID string) (ItemsResult, error) {
	v := url.Values{}
	set(v, "userId", userID)
	v.Set("enableImages", "false")
	var r ItemsResult
	err := c.get(ctx, "Playlists/"+url.PathEscape(playlistID)+"/Items", v, &r)
	return r, err
}

// RemoveFromPlaylist removes entries, identified by PlaylistItemId, from a playlist.
func (c *Client) RemoveFromPlaylist(ctx context.Context, playlistID string, entryIDs []string) error {
	v := url.Values{"entryIds": {strings.Join(entryIDs, ",")}}
	return c.delete(ctx, "Playlists/"+url.PathEscape(playlistID)+"/Items", v)
}

// Play tells a session to play items. command is PlayNow, PlayNext or PlayLast.
func (c *Client) Play(ctx context.Context, sessionID, command string, ids []string, startTicks int64) error {
	v := url.Values{"playCommand": {command}, "itemIds": {strings.Join(ids, ",")}}
	if startTicks > 0 {
		v.Set("startPositionTicks", strconv.FormatInt(startTicks, 10))
	}
	return c.post(ctx, "Sessions/"+url.PathEscape(sessionID)+"/Playing", v, nil, nil)
}

// Playstate sends a playstate command such as Pause or Seek to a session.
func (c *Client) Playstate(ctx context.Context, sessionID, command string, seekTicks int64) error {
	v := url.Values{}
	if command == "Seek" {
		v.Set("seekPositionTicks", strconv.FormatInt(seekTicks, 10))
	}
	return c.post(ctx, "Sessions/"+url.PathEscape(sessionID)+"/Playing/"+command, v, nil, nil)
}

// SendMessage shows a message on a session's screen.
func (c *Client) SendMessage(ctx context.Context, sessionID, header, text string, timeout time.Duration) error {
	body := map[string]any{"Header": header, "Text": text}
	if timeout > 0 {
		body["TimeoutMs"] = timeout.Milliseconds()
	}
	return c.post(ctx, "Sessions/"+url.PathEscape(sessionID)+"/Message", nil, body, nil)
}

// RefreshLibrary starts a scan of all libraries.
func (c *Client) RefreshLibrary(ctx context.Context) error {
	return c.post(ctx, "Library/Refresh", nil, nil, nil)
}

// RefreshItem queues a refresh of an item, or of a library folder and
// everything in it. full fetches metadata and images again; otherwise only
// new and changed files are picked up.
func (c *Client) RefreshItem(ctx context.Context, id string, full bool) error {
	mode := "Default"
	if full {
		mode = "FullRefresh"
	}
	v := url.Values{
		"recursive":           {"true"},
		"metadataRefreshMode": {mode},
		"imageRefreshMode":    {mode},
		"replaceAllMetadata":  {"false"},
		"replaceAllImages":    {"false"},
		"regenerateTrickplay": {"false"},
	}
	return c.post(ctx, "Items/"+url.PathEscape(id)+"/Refresh", v, nil, nil)
}

// ScheduledTasks returns the visible scheduled tasks.
func (c *Client) ScheduledTasks(ctx context.Context) ([]Task, error) {
	var ts []Task
	err := c.get(ctx, "ScheduledTasks", url.Values{"isHidden": {"false"}}, &ts)
	return ts, err
}

// StartTask starts a scheduled task now.
func (c *Client) StartTask(ctx context.Context, id string) error {
	return c.post(ctx, "ScheduledTasks/Running/"+url.PathEscape(id), nil, nil, nil)
}

// ActivityLog returns the newest activity log entries since minDate (zero for all).
func (c *Client) ActivityLog(ctx context.Context, since time.Time, limit int) ([]ActivityEntry, int, error) {
	v := url.Values{"limit": {strconv.Itoa(limit)}}
	if !since.IsZero() {
		v.Set("minDate", since.UTC().Format(time.RFC3339))
	}
	var r activityResult
	err := c.get(ctx, "System/ActivityLog/Entries", v, &r)
	return r.Items, r.TotalRecordCount, err
}

// SystemInfo returns the server's system information.
func (c *Client) SystemInfo(ctx context.Context) (SystemInfo, error) {
	var s SystemInfo
	err := c.get(ctx, "System/Info", nil, &s)
	return s, err
}
