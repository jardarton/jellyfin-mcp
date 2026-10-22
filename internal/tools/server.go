package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jardarton/jellyfin-mcp/internal/jellyfin"
)

func (t *Tools) registerServer(server *mcp.Server) {
	add(t, server, tool{
		name:        "server_info",
		description: "The Jellyfin server's name, version and platform, and whether it needs a restart or has an update.",
		readOnly:    true,
	}, t.serverInfo)
	add(t, server, tool{
		name:        "scheduled_tasks",
		description: "The server's scheduled tasks (library scan, metadata refresh, cleanup and so on): whether they are running and how their last run went.",
		readOnly:    true,
	}, t.scheduledTasks)
	add(t, server, tool{
		name:        "activity_log",
		description: "The server's activity log, newest first: playback, logins, library changes, task failures and warnings.",
		readOnly:    true,
	}, t.activityLog)
	add(t, server, tool{
		name:        "run_task",
		description: "Start a scheduled task now. It runs in the background; follow it with scheduled_tasks.",
	}, t.runTask)
	add(t, server, tool{
		name: "scan_library",
		description: "Scan one library, or all of them, for new, changed and removed files. " +
			"It runs in the background; new items show up in latest as they are found.",
	}, t.scanLibrary)
	add(t, server, tool{
		name: "refresh_metadata",
		description: "Rescan an item's files and look up missing metadata and images from the online providers, " +
			"for example when a poster, overview or new episode is missing. Existing metadata is kept. " +
			"Refreshing a series or season includes its episodes.",
	}, t.refreshMetadata)
}

type serverInfoIn struct{}

type serverInfoOut struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	OS              string `json:"os,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	PendingRestart  bool   `json:"pending_restart"`
	UpdateAvailable bool   `json:"update_available"`
	ShuttingDown    bool   `json:"shutting_down,omitempty"`
}

func (t *Tools) serverInfo(ctx context.Context, _ serverInfoIn) (serverInfoOut, error) {
	s, err := t.c.SystemInfo(ctx)
	if err != nil {
		return serverInfoOut{}, err
	}
	return serverInfoOut{
		Name:            s.ServerName,
		Version:         s.Version,
		OS:              s.OperatingSystemDisplayName,
		Architecture:    s.SystemArchitecture,
		PendingRestart:  s.HasPendingRestart,
		UpdateAvailable: s.HasUpdateAvailable,
		ShuttingDown:    s.IsShuttingDown,
	}, nil
}

type tasksIn struct{}

type taskRun struct {
	Status string `json:"status" jsonschema:"Completed, Failed, Cancelled or Aborted"`
	Start  string `json:"start,omitempty"`
	End    string `json:"end,omitempty"`
	Error  string `json:"error,omitempty"`
}

type taskInfo struct {
	ID       string   `json:"id"`
	Key      string   `json:"key,omitempty"`
	Name     string   `json:"name"`
	Category string   `json:"category,omitempty"`
	State    string   `json:"state" jsonschema:"Idle, Running or Cancelling"`
	Progress float64  `json:"progress_percent,omitempty"`
	LastRun  *taskRun `json:"last_run,omitempty"`
}

type tasksOut struct {
	Tasks []taskInfo `json:"tasks"`
}

func (t *Tools) scheduledTasks(ctx context.Context, _ tasksIn) (tasksOut, error) {
	ts, err := t.c.ScheduledTasks(ctx)
	if err != nil {
		return tasksOut{}, err
	}
	out := tasksOut{Tasks: make([]taskInfo, len(ts))}
	for i, tk := range ts {
		out.Tasks[i] = toTask(tk)
	}
	return out, nil
}

func toTask(tk jellyfin.Task) taskInfo {
	ti := taskInfo{ID: tk.ID, Key: tk.Key, Name: tk.Name, Category: tk.Category, State: tk.State}
	if tk.CurrentProgressPercentage != nil {
		ti.Progress = round1(*tk.CurrentProgressPercentage)
	}
	if r := tk.LastExecutionResult; r != nil {
		ti.LastRun = &taskRun{
			Status: r.Status,
			Start:  timestamp(r.StartTimeUtc),
			End:    timestamp(r.EndTimeUtc),
			Error:  r.ErrorMessage,
		}
	}
	return ti
}

type runTaskIn struct {
	Task string `json:"task" jsonschema:"task key (such as RefreshLibrary), id or name, from scheduled_tasks"`
}

type runTaskOut struct {
	Task taskInfo `json:"task" jsonschema:"the task as it was before starting it"`
}

func (t *Tools) runTask(ctx context.Context, in runTaskIn) (runTaskOut, error) {
	ref := strings.TrimSpace(in.Task)
	ts, err := t.c.ScheduledTasks(ctx)
	if err != nil {
		return runTaskOut{}, err
	}
	for _, tk := range ts {
		if tk.ID == ref || strings.EqualFold(tk.Key, ref) || strings.EqualFold(tk.Name, ref) {
			if tk.State == "Running" {
				return runTaskOut{}, fmt.Errorf("%s is already running", tk.Name)
			}
			if err := t.c.StartTask(ctx, tk.ID); err != nil {
				return runTaskOut{}, err
			}
			return runTaskOut{Task: toTask(tk)}, nil
		}
	}
	return runTaskOut{}, fmt.Errorf("no task %q; list them with scheduled_tasks", ref)
}

type activityIn struct {
	Since string `json:"since,omitempty" jsonschema:"YYYY-MM-DD, an RFC 3339 time, or a duration back from now such as 24h; default 7 days"`
	Limit int    `json:"limit,omitempty" jsonschema:"default 25, at most 200"`
}

type activityEntry struct {
	Date     string `json:"date"`
	Name     string `json:"name"`
	Detail   string `json:"detail,omitempty"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	ItemID   string `json:"item_id,omitempty"`
}

type activityOut struct {
	Entries []activityEntry `json:"entries"`
	Total   int             `json:"total" jsonschema:"entries since then, including those past the limit"`
}

func (t *Tools) activityLog(ctx context.Context, in activityIn) (activityOut, error) {
	since, err := parseSince(in.Since, time.Now())
	if err != nil {
		return activityOut{}, err
	}
	es, total, err := t.c.ActivityLog(ctx, since, clampLimit(in.Limit, 25, 200))
	if err != nil {
		return activityOut{}, err
	}
	out := activityOut{Entries: make([]activityEntry, len(es)), Total: total}
	for i, e := range es {
		detail := e.ShortOverview
		if detail == "" {
			detail = e.Overview
		}
		out.Entries[i] = activityEntry{
			Date:     timestamp(e.Date),
			Name:     e.Name,
			Detail:   detail,
			Type:     e.Type,
			Severity: e.Severity,
			ItemID:   e.ItemID,
		}
	}
	return out, nil
}

func parseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return now.AddDate(0, 0, -7), nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d.Abs()), nil
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("since %q is not a date, time or duration", s)
}

type scanIn struct {
	Library string `json:"library,omitempty" jsonschema:"library name or id; omit to scan all libraries"`
}

type scanOut struct {
	Scanning []string `json:"scanning" jsonschema:"the libraries being scanned"`
}

func (t *Tools) scanLibrary(ctx context.Context, in scanIn) (scanOut, error) {
	if in.Library == "" {
		fs, err := t.c.VirtualFolders(ctx)
		if err != nil {
			return scanOut{}, err
		}
		if err := t.c.RefreshLibrary(ctx); err != nil {
			return scanOut{}, err
		}
		out := scanOut{Scanning: make([]string, len(fs))}
		for i, f := range fs {
			out.Scanning[i] = f.Name
		}
		return out, nil
	}
	lib, err := t.library(ctx, in.Library)
	if err != nil {
		return scanOut{}, err
	}
	if err := t.c.RefreshItem(ctx, lib.ItemID, false); err != nil {
		return scanOut{}, err
	}
	return scanOut{Scanning: []string{lib.Name}}, nil
}

type refreshIn struct {
	Item string `json:"item" jsonschema:"item id, or name with an optional (year)"`
}

type refreshOut struct {
	Refreshing item `json:"refreshing"`
}

func (t *Tools) refreshMetadata(ctx context.Context, in refreshIn) (refreshOut, error) {
	it, err := t.item(ctx, in.Item, "")
	if err != nil {
		return refreshOut{}, err
	}
	if err := t.c.RefreshItem(ctx, it.ID, true); err != nil {
		return refreshOut{}, err
	}
	return refreshOut{Refreshing: toItem(it)}, nil
}
