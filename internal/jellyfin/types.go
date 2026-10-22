package jellyfin

// TicksPerSecond converts Jellyfin's 100 ns ticks.
const TicksPerSecond = 10_000_000

// Item is the subset of BaseItemDto that jellyfin-mcp reads.
type Item struct {
	ID                 string            `json:"Id"`
	Name               string            `json:"Name"`
	OriginalTitle      string            `json:"OriginalTitle"`
	Type               string            `json:"Type"`
	MediaType          string            `json:"MediaType"`
	CollectionType     string            `json:"CollectionType"`
	IsFolder           bool              `json:"IsFolder"`
	ParentID           string            `json:"ParentId"`
	ProductionYear     int               `json:"ProductionYear"`
	PremiereDate       string            `json:"PremiereDate"`
	EndDate            string            `json:"EndDate"`
	DateCreated        string            `json:"DateCreated"`
	Status             string            `json:"Status"`
	Overview           string            `json:"Overview"`
	Taglines           []string          `json:"Taglines"`
	Genres             []string          `json:"Genres"`
	Tags               []string          `json:"Tags"`
	Studios            []NameID          `json:"Studios"`
	People             []Person          `json:"People"`
	CommunityRating    float64           `json:"CommunityRating"`
	CriticRating       float64           `json:"CriticRating"`
	OfficialRating     string            `json:"OfficialRating"`
	RunTimeTicks       int64             `json:"RunTimeTicks"`
	SeriesName         string            `json:"SeriesName"`
	SeriesID           string            `json:"SeriesId"`
	SeasonName         string            `json:"SeasonName"`
	SeasonID           string            `json:"SeasonId"`
	ParentIndexNumber  *int              `json:"ParentIndexNumber"`
	IndexNumber        *int              `json:"IndexNumber"`
	Album              string            `json:"Album"`
	AlbumArtist        string            `json:"AlbumArtist"`
	Artists            []string          `json:"Artists"`
	ChildCount         int               `json:"ChildCount"`
	RecursiveItemCount int               `json:"RecursiveItemCount"`
	Path               string            `json:"Path"`
	ProviderIDs        map[string]string `json:"ProviderIds"`
	MediaStreams       []MediaStream     `json:"MediaStreams"`
	PlaylistItemID     string            `json:"PlaylistItemId"`
	UserData           *UserData         `json:"UserData"`
}

// NameID is a named reference such as a studio.
type NameID struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

// Person is a cast or crew member of an item.
type Person struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
	Role string `json:"Role"`
	Type string `json:"Type"`
}

// MediaStream is one video, audio or subtitle stream of an item.
type MediaStream struct {
	Type         string `json:"Type"`
	Codec        string `json:"Codec"`
	Language     string `json:"Language"`
	DisplayTitle string `json:"DisplayTitle"`
	Width        int    `json:"Width"`
	Height       int    `json:"Height"`
	Channels     int    `json:"Channels"`
	VideoRange   string `json:"VideoRange"`
	IsDefault    bool   `json:"IsDefault"`
	IsForced     bool   `json:"IsForced"`
	IsExternal   bool   `json:"IsExternal"`
}

// UserData is one user's state for an item.
type UserData struct {
	PlaybackPositionTicks int64    `json:"PlaybackPositionTicks"`
	PlayCount             int      `json:"PlayCount"`
	IsFavorite            bool     `json:"IsFavorite"`
	Played                bool     `json:"Played"`
	LastPlayedDate        string   `json:"LastPlayedDate"`
	UnplayedItemCount     *int     `json:"UnplayedItemCount"`
	PlayedPercentage      *float64 `json:"PlayedPercentage"`
}

// ItemsResult is a page of items.
type ItemsResult struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
	StartIndex       int    `json:"StartIndex"`
}

// User is a Jellyfin user account.
type User struct {
	ID               string `json:"Id"`
	Name             string `json:"Name"`
	LastLoginDate    string `json:"LastLoginDate"`
	LastActivityDate string `json:"LastActivityDate"`
	Policy           struct {
		IsAdministrator bool `json:"IsAdministrator"`
		IsDisabled      bool `json:"IsDisabled"`
		IsHidden        bool `json:"IsHidden"`
	} `json:"Policy"`
}

// VirtualFolder is a library as configured on the server.
type VirtualFolder struct {
	Name           string   `json:"Name"`
	ItemID         string   `json:"ItemId"`
	CollectionType string   `json:"CollectionType"`
	Locations      []string `json:"Locations"`
	RefreshStatus  string   `json:"RefreshStatus"`
}

// ItemCounts are the server-wide item counts.
type ItemCounts struct {
	MovieCount      int `json:"MovieCount"`
	SeriesCount     int `json:"SeriesCount"`
	EpisodeCount    int `json:"EpisodeCount"`
	ArtistCount     int `json:"ArtistCount"`
	AlbumCount      int `json:"AlbumCount"`
	SongCount       int `json:"SongCount"`
	MusicVideoCount int `json:"MusicVideoCount"`
	BoxSetCount     int `json:"BoxSetCount"`
	BookCount       int `json:"BookCount"`
	ProgramCount    int `json:"ProgramCount"`
	TrailerCount    int `json:"TrailerCount"`
	ItemCount       int `json:"ItemCount"`
}

// Session is a connected client.
type Session struct {
	ID                    string           `json:"Id"`
	UserID                string           `json:"UserId"`
	UserName              string           `json:"UserName"`
	Client                string           `json:"Client"`
	DeviceName            string           `json:"DeviceName"`
	DeviceID              string           `json:"DeviceId"`
	ApplicationVersion    string           `json:"ApplicationVersion"`
	LastActivityDate      string           `json:"LastActivityDate"`
	IsActive              bool             `json:"IsActive"`
	SupportsRemoteControl bool             `json:"SupportsRemoteControl"`
	SupportsMediaControl  bool             `json:"SupportsMediaControl"`
	PlayableMediaTypes    []string         `json:"PlayableMediaTypes"`
	NowPlayingItem        *Item            `json:"NowPlayingItem"`
	PlayState             *PlayState       `json:"PlayState"`
	TranscodingInfo       *TranscodingInfo `json:"TranscodingInfo"`
}

// PlayState is a session's playback state.
type PlayState struct {
	PositionTicks int64  `json:"PositionTicks"`
	IsPaused      bool   `json:"IsPaused"`
	IsMuted       bool   `json:"IsMuted"`
	VolumeLevel   *int   `json:"VolumeLevel"`
	PlayMethod    string `json:"PlayMethod"`
}

// TranscodingInfo describes an active transcode.
type TranscodingInfo struct {
	VideoCodec               string   `json:"VideoCodec"`
	AudioCodec               string   `json:"AudioCodec"`
	Container                string   `json:"Container"`
	IsVideoDirect            bool     `json:"IsVideoDirect"`
	IsAudioDirect            bool     `json:"IsAudioDirect"`
	Bitrate                  int      `json:"Bitrate"`
	Width                    int      `json:"Width"`
	Height                   int      `json:"Height"`
	HardwareAccelerationType string   `json:"HardwareAccelerationType"`
	TranscodeReasons         []string `json:"TranscodeReasons"`
}

// SystemInfo is the server's /System/Info.
type SystemInfo struct {
	ServerName                 string `json:"ServerName"`
	Version                    string `json:"Version"`
	ProductName                string `json:"ProductName"`
	OperatingSystemDisplayName string `json:"OperatingSystemDisplayName"`
	SystemArchitecture         string `json:"SystemArchitecture"`
	HasPendingRestart          bool   `json:"HasPendingRestart"`
	HasUpdateAvailable         bool   `json:"HasUpdateAvailable"`
	IsShuttingDown             bool   `json:"IsShuttingDown"`
	StartupWizardCompleted     *bool  `json:"StartupWizardCompleted"`
}

// Task is a scheduled task.
type Task struct {
	ID                        string      `json:"Id"`
	Key                       string      `json:"Key"`
	Name                      string      `json:"Name"`
	Description               string      `json:"Description"`
	Category                  string      `json:"Category"`
	State                     string      `json:"State"`
	CurrentProgressPercentage *float64    `json:"CurrentProgressPercentage"`
	LastExecutionResult       *TaskResult `json:"LastExecutionResult"`
}

// TaskResult is the outcome of a task's last run.
type TaskResult struct {
	StartTimeUtc string `json:"StartTimeUtc"`
	EndTimeUtc   string `json:"EndTimeUtc"`
	Status       string `json:"Status"`
	ErrorMessage string `json:"ErrorMessage"`
}

// ActivityEntry is one entry in the activity log.
type ActivityEntry struct {
	ID            int64  `json:"Id"`
	Name          string `json:"Name"`
	ShortOverview string `json:"ShortOverview"`
	Overview      string `json:"Overview"`
	Type          string `json:"Type"`
	ItemID        string `json:"ItemId"`
	UserID        string `json:"UserId"`
	Date          string `json:"Date"`
	Severity      string `json:"Severity"`
}

type activityResult struct {
	Items            []ActivityEntry `json:"Items"`
	TotalRecordCount int             `json:"TotalRecordCount"`
}
