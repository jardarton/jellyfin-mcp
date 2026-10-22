package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jardarton/jellyfin-mcp/internal/jellyfin"
)

// playCommands and stateCommands map control_playback commands to Jellyfin's.
var (
	playCommands = map[string]string{
		"play":      "PlayNow",
		"play_next": "PlayNext",
		"play_last": "PlayLast",
	}
	stateCommands = map[string]string{
		"pause":        "Pause",
		"unpause":      "Unpause",
		"toggle_pause": "PlayPause",
		"stop":         "Stop",
		"next":         "NextTrack",
		"previous":     "PreviousTrack",
		"seek":         "Seek",
		"rewind":       "Rewind",
		"fast_forward": "FastForward",
	}
)

// recentSessions is how far back sessions lists by default.
const recentSessions = 15 * time.Minute

func (t *Tools) registerSessions(server *mcp.Server) {
	add(t, server, tool{
		name: "sessions",
		description: "Clients connected to the server: who is watching or listening to what on which device, how far in, " +
			"whether it is paused or transcoding and why, and whether the client can be remote controlled.",
		readOnly: true,
	}, t.sessions)

	var cmds []string
	for c := range playCommands {
		cmds = append(cmds, c)
	}
	for c := range stateCommands {
		cmds = append(cmds, c)
	}
	slices.Sort(cmds)
	add(t, server, tool{
		name: "control_playback",
		description: "Remote control a session: play items now (replacing what is playing), queue them next or last, " +
			"pause, unpause, stop, skip, or seek to a position. The session must support remote control.",
		enums: map[string][]string{"command": cmds},
	}, t.controlPlayback)
	add(t, server, tool{
		name:        "send_message",
		description: "Show a message on a session's screen.",
	}, t.sendMessage)
}

type sessionsIn struct {
	All bool `json:"all,omitempty" jsonschema:"include sessions idle for more than 15 minutes"`
}

type nowPlaying struct {
	item
	Position    string       `json:"position,omitempty" jsonschema:"h:mm:ss"`
	Paused      bool         `json:"paused"`
	PlayMethod  string       `json:"play_method,omitempty" jsonschema:"DirectPlay, DirectStream or Transcode"`
	Transcoding *transcoding `json:"transcoding,omitempty"`
}

type transcoding struct {
	Video         string   `json:"video,omitempty" jsonschema:"target video codec, or direct"`
	Audio         string   `json:"audio,omitempty" jsonschema:"target audio codec, or direct"`
	Container     string   `json:"container,omitempty"`
	BitrateKbps   int      `json:"bitrate_kbps,omitempty"`
	HardwareAccel string   `json:"hardware_acceleration,omitempty"`
	Reasons       []string `json:"reasons,omitempty"`
}

type sessionInfo struct {
	ID            string      `json:"id"`
	User          string      `json:"user,omitempty"`
	Client        string      `json:"client"`
	Device        string      `json:"device"`
	Version       string      `json:"version,omitempty"`
	LastActivity  string      `json:"last_activity,omitempty"`
	RemoteControl bool        `json:"remote_control" jsonschema:"control_playback and send_message work on this session"`
	NowPlaying    *nowPlaying `json:"now_playing,omitempty"`
}

type sessionsOut struct {
	Sessions []sessionInfo `json:"sessions"`
}

func (t *Tools) sessions(ctx context.Context, in sessionsIn) (sessionsOut, error) {
	within := recentSessions
	if in.All {
		within = 0
	}
	ss, err := t.c.Sessions(ctx, within)
	if err != nil {
		return sessionsOut{}, err
	}
	out := sessionsOut{Sessions: make([]sessionInfo, 0, len(ss))}
	for _, s := range ss {
		out.Sessions = append(out.Sessions, toSession(s))
	}
	return out, nil
}

func toSession(s jellyfin.Session) sessionInfo {
	si := sessionInfo{
		ID:            s.ID,
		User:          s.UserName,
		Client:        s.Client,
		Device:        s.DeviceName,
		Version:       s.ApplicationVersion,
		LastActivity:  timestamp(s.LastActivityDate),
		RemoteControl: s.SupportsRemoteControl,
	}
	if s.NowPlayingItem == nil {
		return si
	}
	np := &nowPlaying{item: toItem(*s.NowPlayingItem)}
	if ps := s.PlayState; ps != nil {
		np.Position = clock(ps.PositionTicks)
		np.Paused = ps.IsPaused
		np.PlayMethod = ps.PlayMethod
	}
	if tc := s.TranscodingInfo; tc != nil && np.PlayMethod == "Transcode" {
		np.Transcoding = &transcoding{
			Video:         direct(tc.IsVideoDirect, tc.VideoCodec),
			Audio:         direct(tc.IsAudioDirect, tc.AudioCodec),
			Container:     tc.Container,
			BitrateKbps:   tc.Bitrate / 1000,
			HardwareAccel: tc.HardwareAccelerationType,
			Reasons:       tc.TranscodeReasons,
		}
	}
	si.NowPlaying = np
	return si
}

func direct(isDirect bool, codec string) string {
	if isDirect {
		return "direct"
	}
	return codec
}

type controlIn struct {
	Session  string   `json:"session" jsonschema:"session id, or the device or client name"`
	Command  string   `json:"command"`
	Items    []string `json:"items,omitempty" jsonschema:"for play, play_next and play_last: item ids, or names with an optional (year)"`
	Position string   `json:"position,omitempty" jsonschema:"for seek, and optionally play: h:mm:ss from the start"`
}

type controlOut struct {
	Session sessionInfo `json:"session" jsonschema:"the session as it was before the command"`
	Sent    string      `json:"sent"`
	Items   []item      `json:"items,omitempty"`
}

func (t *Tools) controlPlayback(ctx context.Context, in controlIn) (controlOut, error) {
	s, err := t.session(ctx, in.Session)
	if err != nil {
		return controlOut{}, err
	}
	if !s.SupportsRemoteControl {
		return controlOut{}, fmt.Errorf("%s on %s does not support remote control", s.Client, s.DeviceName)
	}
	var pos int64
	if in.Position != "" {
		if pos, err = parseClock(in.Position); err != nil {
			return controlOut{}, err
		}
	}
	out := controlOut{Session: toSession(s)}

	if cmd, ok := playCommands[in.Command]; ok {
		its, err := t.items(ctx, in.Items, s.UserID)
		if err != nil {
			return controlOut{}, err
		}
		if err := t.c.Play(ctx, s.ID, cmd, ids(its), pos); err != nil {
			return controlOut{}, err
		}
		out.Sent, out.Items = cmd, toItems(its)
		return out, nil
	}

	cmd, ok := stateCommands[in.Command]
	if !ok {
		return controlOut{}, fmt.Errorf("unknown command %q", in.Command)
	}
	if cmd == "Seek" && in.Position == "" {
		return controlOut{}, fmt.Errorf("seek needs a position")
	}
	if err := t.c.Playstate(ctx, s.ID, cmd, pos); err != nil {
		return controlOut{}, err
	}
	out.Sent = cmd
	return out, nil
}

type messageIn struct {
	Session        string `json:"session" jsonschema:"session id, or the device or client name"`
	Text           string `json:"text"`
	Header         string `json:"header,omitempty" jsonschema:"title shown above the text"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"hide the message after this long; omit to leave it until dismissed"`
}

type messageOut struct {
	Session sessionInfo `json:"session"`
}

func (t *Tools) sendMessage(ctx context.Context, in messageIn) (messageOut, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return messageOut{}, fmt.Errorf("text is required")
	}
	s, err := t.session(ctx, in.Session)
	if err != nil {
		return messageOut{}, err
	}
	if !s.SupportsRemoteControl {
		return messageOut{}, fmt.Errorf("%s on %s does not support remote control", s.Client, s.DeviceName)
	}
	timeout := time.Duration(max(in.TimeoutSeconds, 0)) * time.Second
	if err := t.c.SendMessage(ctx, s.ID, in.Header, text, timeout); err != nil {
		return messageOut{}, err
	}
	return messageOut{Session: toSession(s)}, nil
}
