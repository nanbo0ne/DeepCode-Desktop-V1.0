package hosttools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/documentextract"
	fileenc "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/fileutil/encoding"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/notify"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/product"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

// Tools returns the default host-tool library. It intentionally excludes visual
// desktop control, OCR, screenshots, and coordinate input.
func Tools(workDir string, settingsOpt ...config.ToolLibraryConfig) []tool.Tool {
	settings := config.DefaultToolLibrarySettings()
	if len(settingsOpt) > 0 {
		settings = config.NormalizeToolLibrarySettings(settingsOpt[0])
	}
	rt := newRuntimeManager(workDir)
	out := []tool.Tool{
		automationCreate{workDir: workDir},
		automationList{},
		automationCancel{},
	}
	if settings.HostSystemToolsEnabled {
		out = append(out,
			hostCommand{workDir: workDir},
			hostSystemInfo{workDir: workDir},
			hostListProcesses{},
			hostKillProcess{},
			hostOpenApp{workDir: workDir},
			hostClipboard{},
			notifyUser{},
		)
	}
	if settings.ThreadManagementEnabled {
		out = append(out, threadList{})
	}
	if settings.WebSearchEnabled {
		out = append(out, webSearch{})
	}
	if settings.REPLRuntimeEnabled {
		out = append(out, nodeRepl{rt: rt}, pythonRepl{rt: rt})
	}
	if settings.DocumentToolsEnabled {
		out = append(out, documentInspect{workDir: workDir}, documentExtract{workDir: workDir})
	}
	if settings.ConversationSearchEnabled {
		out = append(out, conversationSearch{workDir: workDir}, conversationRead{workDir: workDir})
	}
	return out
}

var automations = newAutomationStore()

// AutomationView is shared by the tool output and desktop management UI.
type AutomationView struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Kind            string `json:"kind"`
	Schedule        string `json:"schedule"`
	Action          string `json:"action"`
	CreatedAt       string `json:"createdAt"`
	LastRunAt       string `json:"lastRunAt,omitempty"`
	NextRunAt       string `json:"nextRunAt,omitempty"`
	Status          string `json:"status"`
	Result          string `json:"result,omitempty"`
	Error           string `json:"error,omitempty"`
	IntervalSeconds int    `json:"intervalSeconds,omitempty"`
	DailyTime       string `json:"dailyTime,omitempty"`
	WeeklyDay       string `json:"weeklyDay,omitempty"`
	WeeklyTime      string `json:"weeklyTime,omitempty"`
	Monitor         string `json:"monitor,omitempty"`
}

type automationStore struct {
	mu     sync.Mutex
	loaded bool
	next   int
	items  map[string]*automationItem
}

type automationItem struct {
	ID              string    `json:"id"`
	Label           string    `json:"label"`
	Kind            string    `json:"kind"`
	Action          string    `json:"action"`
	Command         string    `json:"command,omitempty"`
	Message         string    `json:"message,omitempty"`
	IntervalSeconds int       `json:"interval_seconds,omitempty"`
	DailyTime       string    `json:"daily_time,omitempty"`
	WeeklyDay       string    `json:"weekly_day,omitempty"`
	WeeklyTime      string    `json:"weekly_time,omitempty"`
	Monitor         string    `json:"monitor,omitempty"`
	WorkDir         string    `json:"workDir,omitempty"`
	NextRunAt       time.Time `json:"nextRunAt,omitempty"`
	LastRunAt       time.Time `json:"lastRunAt,omitempty"`
	Status          string    `json:"status"`
	Result          string    `json:"result,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	cancel          context.CancelFunc
}

func newAutomationStore() *automationStore {
	return &automationStore{items: map[string]*automationItem{}}
}

func automationFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = home
	}
	return filepath.Join(dir, product.ConfigDirName, "desktop-automations.json")
}

func (s *automationStore) ensureLoadedLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	b, err := os.ReadFile(automationFilePath())
	if err != nil {
		return
	}
	var items []*automationItem
	if err := json.Unmarshal(b, &items); err != nil {
		return
	}
	now := time.Now()
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.ID) == "" {
			continue
		}
		item.cancel = nil
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		if item.Status == "running" {
			item.Status = "scheduled"
		}
		if item.Status == "scheduled" && item.NextRunAt.IsZero() {
			item.NextRunAt = nextAutomationRun(item, now)
		}
		s.items[item.ID] = item
		if strings.HasPrefix(item.ID, "automation-") {
			if n, err := strconv.Atoi(strings.TrimPrefix(item.ID, "automation-")); err == nil && n > s.next {
				s.next = n
			}
		}
	}
	for _, item := range s.items {
		if item.Status == "scheduled" {
			s.startLocked(item)
		}
	}
}

func (s *automationStore) persistLocked() {
	path := automationFilePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	items := make([]automationItem, 0, len(s.items))
	for _, item := range s.items {
		copy := *item
		copy.cancel = nil
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	b, _ := json.MarshalIndent(items, "", "  ")
	_ = os.WriteFile(path, b, 0o644)
}

func (s *automationStore) add(item *automationItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	s.next++
	item.ID = fmt.Sprintf("automation-%d", s.next)
	item.CreatedAt = time.Now()
	item.Status = "scheduled"
	item.NextRunAt = nextAutomationRun(item, time.Now())
	s.items[item.ID] = item
	s.startLocked(item)
	s.persistLocked()
}

func (s *automationStore) list() []automationItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	out := make([]automationItem, 0, len(s.items))
	for _, item := range s.items {
		copy := *item
		copy.cancel = nil
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return automationStatusRank(out[i].Status) < automationStatusRank(out[j].Status)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *automationStore) startLocked(item *automationItem) {
	if item.cancel != nil || item.Status != "scheduled" {
		return
	}
	runCtx, cancel := context.WithCancel(context.Background())
	item.cancel = cancel
	go runAutomation(runCtx, item)
}

func (s *automationStore) cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	item := s.items[id]
	if item == nil {
		return false
	}
	item.Status = "cancelled"
	if item.cancel != nil {
		item.cancel()
		item.cancel = nil
	}
	item.NextRunAt = time.Time{}
	s.persistLocked()
	return true
}

func (s *automationStore) pause(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	item := s.items[id]
	if item == nil || item.Status != "scheduled" {
		return false
	}
	item.Status = "paused"
	if item.cancel != nil {
		item.cancel()
		item.cancel = nil
	}
	s.persistLocked()
	return true
}

func (s *automationStore) resume(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	item := s.items[id]
	if item == nil || item.Status != "paused" {
		return false
	}
	item.Status = "scheduled"
	item.NextRunAt = nextAutomationRun(item, time.Now())
	s.startLocked(item)
	s.persistLocked()
	return true
}

func (s *automationStore) clearFinished() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	n := 0
	for id, item := range s.items {
		switch item.Status {
		case "failed", "cancelled", "done":
			if item.cancel != nil {
				item.cancel()
			}
			delete(s.items, id)
			n++
		}
	}
	if n > 0 {
		s.persistLocked()
	}
	return n
}

func automationStatusRank(status string) int {
	switch status {
	case "running":
		return 0
	case "scheduled":
		return 1
	case "paused":
		return 2
	case "failed":
		return 3
	case "cancelled":
		return 4
	default:
		return 5
	}
}

type automationCreate struct{ workDir string }

func (automationCreate) Name() string { return "automation_create" }
func (automationCreate) Description() string {
	return "Create a persistent automation only for clearly recurring, continuous, or background-monitoring tasks. Use action=notify for repeated reminders and action=host_command for repeated native host commands. Do not use this tool unless the user explicitly asks for a recurring/continuous/monitoring automation. The automation records status/result for automation_list and the desktop automation manager."
}
func (automationCreate) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","description":"Short user-facing automation name."},"interval_seconds":{"type":"integer","minimum":60,"description":"Run repeatedly every N seconds."},"daily_time":{"type":"string","description":"Run every day at HH:MM local time."},"weekly_day":{"type":"string","enum":["monday","tuesday","wednesday","thursday","friday","saturday","sunday"],"description":"Run weekly on this day; requires weekly_time."},"weekly_time":{"type":"string","description":"Run weekly at HH:MM local time; requires weekly_day."},"monitor":{"type":"string","description":"Continuous/background monitoring intent label; combine with interval_seconds."},"action":{"type":"string","enum":["notify","host_command"],"description":"Structured recurring automation action."},"message":{"type":"string","description":"Notification body for notify action."},"command":{"type":"string","description":"Native host command for host_command action. Prefer host tools directly for immediate system actions."}},"required":["action"]}`)
}
func (automationCreate) ReadOnly() bool { return false }
func (a automationCreate) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Label           string `json:"label"`
		IntervalSeconds int    `json:"interval_seconds"`
		DailyTime       string `json:"daily_time"`
		WeeklyDay       string `json:"weekly_day"`
		WeeklyTime      string `json:"weekly_time"`
		Monitor         string `json:"monitor"`
		Action          string `json:"action"`
		Message         string `json:"message"`
		Command         string `json:"command"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Label == "" {
		p.Label = p.Action
	}
	kind, err := automationKind(p.IntervalSeconds, p.DailyTime, p.WeeklyDay, p.WeeklyTime, p.Monitor)
	if err != nil {
		return "", err
	}
	if p.Action == "notify" && strings.TrimSpace(p.Message) == "" {
		return "", fmt.Errorf("message is required for notify automation")
	}
	if p.Action == "host_command" && strings.TrimSpace(p.Command) == "" {
		return "", fmt.Errorf("command is required for host_command automation")
	}
	item := &automationItem{
		Label:           p.Label,
		Kind:            kind,
		Action:          p.Action,
		Command:         p.Command,
		Message:         p.Message,
		IntervalSeconds: p.IntervalSeconds,
		DailyTime:       p.DailyTime,
		WeeklyDay:       strings.ToLower(strings.TrimSpace(p.WeeklyDay)),
		WeeklyTime:      p.WeeklyTime,
		Monitor:         p.Monitor,
		WorkDir:         a.workDir,
	}
	automations.add(item)
	_ = ctx
	return fmt.Sprintf("status=done action=automation_create id=%s label=%q schedule=%s next_run_at=%s", item.ID, item.Label, automationScheduleLabel(item), item.NextRunAt.Format(time.RFC3339)), nil
}

func automationKind(interval int, dailyTime, weeklyDay, weeklyTime, monitor string) (string, error) {
	hasInterval := interval > 0
	hasDaily := strings.TrimSpace(dailyTime) != ""
	hasWeekly := strings.TrimSpace(weeklyDay) != "" || strings.TrimSpace(weeklyTime) != ""
	hasMonitor := strings.TrimSpace(monitor) != ""
	if hasMonitor {
		if !hasInterval {
			return "", fmt.Errorf("monitor automations require interval_seconds")
		}
		return "monitor", nil
	}
	if hasInterval {
		return "interval", nil
	}
	if hasDaily {
		if _, err := parseClock(dailyTime); err != nil {
			return "", fmt.Errorf("daily_time must be HH:MM: %w", err)
		}
		return "daily", nil
	}
	if hasWeekly {
		if strings.TrimSpace(weeklyDay) == "" || strings.TrimSpace(weeklyTime) == "" {
			return "", fmt.Errorf("weekly automations require weekly_day and weekly_time")
		}
		if parseWeekday(weeklyDay) < 0 {
			return "", fmt.Errorf("weekly_day must be monday..sunday")
		}
		if _, err := parseClock(weeklyTime); err != nil {
			return "", fmt.Errorf("weekly_time must be HH:MM: %w", err)
		}
		return "weekly", nil
	}
	return "", fmt.Errorf("automation_create requires a recurring or continuous schedule: interval_seconds, daily_time, weekly_day+weekly_time, or monitor")
}

func runAutomation(ctx context.Context, item *automationItem) {
	for {
		wait := time.Until(item.NextRunAt)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
		finish, err := acquireAutomationWork(ctx)
		if err != nil {
			return
		}
		automations.mu.Lock()
		if item.Status != "scheduled" || ctx.Err() != nil {
			item.cancel = nil
			automations.mu.Unlock()
			finish()
			return
		}
		item.Status = "running"
		item.LastRunAt = time.Now()
		automations.persistLocked()
		automations.mu.Unlock()

		result, runErr := executeAutomation(ctx, item)

		automations.mu.Lock()
		if runErr != nil {
			item.Status = "failed"
			item.Error = runErr.Error()
			item.Result = result
			item.cancel = nil
			automations.persistLocked()
			automations.mu.Unlock()
			finish()
			return
		}
		item.Status = "scheduled"
		item.Error = ""
		item.Result = result
		item.NextRunAt = nextAutomationRun(item, time.Now())
		automations.persistLocked()
		automations.mu.Unlock()
		finish()
	}
}

func executeAutomation(ctx context.Context, item *automationItem) (string, error) {
	switch item.Action {
	case "notify":
		err := notify.NewPlatformSender().Send(notify.Message{Title: firstNonEmpty(item.Label, "O.R.C.A Automation"), Body: item.Message})
		return "status=done action=notify result=notification_sent", err
	case "host_command":
		argv := nativeShellArgv("auto", item.Command)
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		proc.HideWindow(cmd)
		cmd.Dir = item.WorkDir
		out, runErr := cmd.CombinedOutput()
		decoded := strings.TrimSpace(decodeOutput(out))
		result := strings.TrimSpace("status=done action=host_command shell=auto\n" + decoded)
		if runErr != nil {
			return result, runErr
		}
		_ = notify.NewPlatformSender().Send(notify.Message{Title: "O.R.C.A Automation Done", Body: item.Label})
		return result, nil
	default:
		return "", fmt.Errorf("unknown automation action %q", item.Action)
	}
}

func nextAutomationRun(item *automationItem, now time.Time) time.Time {
	switch item.Kind {
	case "interval", "monitor":
		seconds := item.IntervalSeconds
		if seconds < 60 {
			seconds = 60
		}
		return now.Add(time.Duration(seconds) * time.Second)
	case "daily":
		return nextDailyRun(now, item.DailyTime)
	case "weekly":
		return nextWeeklyRun(now, item.WeeklyDay, item.WeeklyTime)
	default:
		return now.Add(time.Hour)
	}
}

func parseClock(value string) (time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid clock")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute")
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

func nextDailyRun(now time.Time, clock string) time.Time {
	offset, err := parseClock(clock)
	if err != nil {
		return now.Add(24 * time.Hour)
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(offset)
	if !start.After(now) {
		start = start.Add(24 * time.Hour)
	}
	return start
}

func parseWeekday(value string) time.Weekday {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sunday":
		return time.Sunday
	case "monday":
		return time.Monday
	case "tuesday":
		return time.Tuesday
	case "wednesday":
		return time.Wednesday
	case "thursday":
		return time.Thursday
	case "friday":
		return time.Friday
	case "saturday":
		return time.Saturday
	default:
		return -1
	}
}

func nextWeeklyRun(now time.Time, day, clock string) time.Time {
	offset, err := parseClock(clock)
	targetDay := parseWeekday(day)
	if err != nil || targetDay < 0 {
		return now.Add(7 * 24 * time.Hour)
	}
	days := (int(targetDay) - int(now.Weekday()) + 7) % 7
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(time.Duration(days)*24*time.Hour + offset)
	if !start.After(now) {
		start = start.Add(7 * 24 * time.Hour)
	}
	return start
}

func automationScheduleLabel(item *automationItem) string {
	switch item.Kind {
	case "monitor":
		return fmt.Sprintf("monitor every %ds: %s", item.IntervalSeconds, item.Monitor)
	case "interval":
		return fmt.Sprintf("every %ds", item.IntervalSeconds)
	case "daily":
		return "daily " + item.DailyTime
	case "weekly":
		return "weekly " + item.WeeklyDay + " " + item.WeeklyTime
	default:
		return item.Kind
	}
}

type automationList struct{}

func (automationList) Name() string { return "automation_list" }
func (automationList) Description() string {
	return "List persistent recurring/continuous automations."
}
func (automationList) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (automationList) ReadOnly() bool { return true }
func (automationList) Execute(context.Context, json.RawMessage) (string, error) {
	b, _ := json.MarshalIndent(ListAutomations(), "", "  ")
	return string(b), nil
}

type automationCancel struct{}

func (automationCancel) Name() string { return "automation_cancel" }
func (automationCancel) Description() string {
	return "Cancel a persistent automation."
}
func (automationCancel) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)
}
func (automationCancel) ReadOnly() bool { return false }
func (automationCancel) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if automations.cancel(p.ID) {
		return "status=cancelled result=automation_cancelled", nil
	}
	return "status=not_found result=automation_not_found", nil
}

// ListAutomations returns automation state for desktop management.
func ListAutomations() []AutomationView {
	items := automations.list()
	out := make([]AutomationView, 0, len(items))
	for _, item := range items {
		out = append(out, automationView(item))
	}
	return out
}

func PauseAutomation(id string) bool  { return automations.pause(id) }
func ResumeAutomation(id string) bool { return automations.resume(id) }
func CancelAutomation(id string) bool { return automations.cancel(id) }
func ClearFinishedAutomations() int   { return automations.clearFinished() }

func automationView(item automationItem) AutomationView {
	view := AutomationView{
		ID:              item.ID,
		Label:           item.Label,
		Kind:            item.Kind,
		Schedule:        automationScheduleLabel(&item),
		Action:          item.Action,
		Status:          item.Status,
		Result:          item.Result,
		Error:           item.Error,
		IntervalSeconds: item.IntervalSeconds,
		DailyTime:       item.DailyTime,
		WeeklyDay:       item.WeeklyDay,
		WeeklyTime:      item.WeeklyTime,
		Monitor:         item.Monitor,
	}
	if !item.CreatedAt.IsZero() {
		view.CreatedAt = item.CreatedAt.Format(time.RFC3339)
	}
	if !item.LastRunAt.IsZero() {
		view.LastRunAt = item.LastRunAt.Format(time.RFC3339)
	}
	if !item.NextRunAt.IsZero() {
		view.NextRunAt = item.NextRunAt.Format(time.RFC3339)
	}
	return view
}

type threadList struct{}

func (threadList) Name() string { return "thread_list" }
func (threadList) Description() string {
	return "List saved Orca conversation threads/topics from the local session store. This manages Orca sessions, not Codex threads."
}
func (threadList) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":200}}}`)
}
func (threadList) ReadOnly() bool { return true }
func (threadList) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	root := config.SessionDir()
	var rows []map[string]any
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return nil
		}
		rows = append(rows, map[string]any{"path": path, "modified": st.ModTime().Format(time.RFC3339), "sizeBytes": st.Size()})
		return nil
	})
	sort.Slice(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i]["modified"]) > fmt.Sprint(rows[j]["modified"])
	})
	if len(rows) > p.Limit {
		rows = rows[:p.Limit]
	}
	b, _ := json.MarshalIndent(rows, "", "  ")
	return string(b), nil
}

type conversationSearch struct{ workDir string }

func (conversationSearch) Name() string { return "conversation_search" }
func (conversationSearch) Description() string {
	return "Search older local Orca conversation transcripts after context compression. Returns short snippets and locators; use conversation_read for fuller nearby transcript. Read-only."
}
func (conversationSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Words or phrase to search for in user/assistant conversation text."},"scope":{"type":"string","enum":["current_workspace","all"],"description":"current_workspace searches sessions for the active workspace plus legacy/global sessions; all searches every known local session directory."},"limit":{"type":"integer","minimum":1,"maximum":20}},"required":["query"]}`)
}
func (conversationSearch) ReadOnly() bool { return true }
func (c conversationSearch) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
		Scope string `json:"scope"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	p.Query = strings.TrimSpace(p.Query)
	if p.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.Limit <= 0 || p.Limit > 20 {
		p.Limit = 8
	}
	dirs := conversationSessionDirs(c.workDir, p.Scope)
	hits := conversationSearchHits(p.Query, dirs, p.Limit)
	out := map[string]any{
		"status": "done",
		"query":  p.Query,
		"scope":  normalizedConversationScope(p.Scope),
		"hits":   hits,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

type conversationRead struct{ workDir string }

func (conversationRead) Name() string { return "conversation_read" }
func (conversationRead) Description() string {
	return "Read a fuller nearby transcript window for a locator returned by conversation_search. Read-only and limited to local Orca session files."
}
func (conversationRead) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"locator":{"type":"string","description":"A locator returned by conversation_search, formatted as path#index."},"before":{"type":"integer","minimum":0,"maximum":12},"after":{"type":"integer","minimum":0,"maximum":12}},"required":["locator"]}`)
}
func (conversationRead) ReadOnly() bool { return true }
func (c conversationRead) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Locator string `json:"locator"`
		Before  int    `json:"before"`
		After   int    `json:"after"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	path, index, err := parseConversationLocator(p.Locator)
	if err != nil {
		return "", err
	}
	if !conversationPathAllowed(path, conversationSessionDirs(c.workDir, "all")) {
		return "", fmt.Errorf("locator is outside known Orca session directories")
	}
	sess, err := agent.LoadSession(path)
	if err != nil {
		return "", err
	}
	if index < 0 || index >= len(sess.Messages) {
		return "", fmt.Errorf("locator message index %d is outside transcript length %d", index, len(sess.Messages))
	}
	if p.Before <= 0 {
		p.Before = 2
	}
	if p.After <= 0 {
		p.After = 3
	}
	if p.Before > 12 {
		p.Before = 12
	}
	if p.After > 12 {
		p.After = 12
	}
	start := index - p.Before
	if start < 0 {
		start = 0
	}
	end := index + p.After + 1
	if end > len(sess.Messages) {
		end = len(sess.Messages)
	}
	rows := make([]map[string]any, 0, end-start)
	for i := start; i < end; i++ {
		m := sess.Messages[i]
		if !conversationMessageVisible(m) && i != index {
			continue
		}
		rows = append(rows, map[string]any{
			"index":   i,
			"role":    string(m.Role),
			"name":    m.Name,
			"content": trimConversationText(conversationMessageText(m), 6000),
		})
	}
	out := map[string]any{
		"status":   "done",
		"locator":  conversationLocator(path, index),
		"path":     path,
		"messages": rows,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

func conversationSearchHits(query string, dirs []string, limit int) []map[string]any {
	needle := strings.ToLower(query)
	var hits []map[string]any
	for _, path := range conversationSessionFiles(dirs) {
		sess, err := agent.LoadSession(path)
		if err != nil {
			continue
		}
		for i, m := range sess.Messages {
			if !conversationMessageVisible(m) {
				continue
			}
			text := conversationMessageText(m)
			if !strings.Contains(strings.ToLower(text), needle) {
				continue
			}
			hits = append(hits, map[string]any{
				"locator": conversationLocator(path, i),
				"path":    path,
				"index":   i,
				"role":    string(m.Role),
				"name":    m.Name,
				"snippet": conversationSnippet(text, needle, 360),
			})
			if len(hits) >= limit {
				return hits
			}
		}
	}
	return hits
}

func conversationSessionDirs(workDir, scope string) []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	add(config.ProjectSessionDir(workDir))
	add(config.SessionDir())
	if normalizedConversationScope(scope) == "all" {
		base := config.MemoryUserDir()
		for _, root := range []string{
			filepath.Join(base, "projects"),
			filepath.Join(base, product.LegacyConfigDirName, "independent-workspaces"),
			filepath.Join(base, "independent-workspaces"),
		} {
			_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil || !d.IsDir() || filepath.Base(path) != "sessions" {
					return nil
				}
				add(path)
				return filepath.SkipDir
			})
		}
	}
	return dirs
}

func normalizedConversationScope(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), "all") {
		return "all"
	}
	return "current_workspace"
}

func conversationSessionFiles(dirs []string) []string {
	var files []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
				continue
			}
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Slice(files, func(i, j int) bool {
		ii, ierr := os.Stat(files[i])
		ji, jerr := os.Stat(files[j])
		if ierr != nil || jerr != nil {
			return files[i] < files[j]
		}
		return ii.ModTime().After(ji.ModTime())
	})
	return files
}

func conversationMessageVisible(m provider.Message) bool {
	text := strings.TrimSpace(m.Content)
	if text == "" && m.Role != provider.RoleTool {
		return false
	}
	if m.Role != provider.RoleUser && m.Role != provider.RoleAssistant && m.Role != provider.RoleTool {
		return false
	}
	for _, prefix := range []string{"<system-reminder>", "<workflow-reminder>", "<context-checkpoint>", "<memory-update>"} {
		if strings.HasPrefix(text, prefix) {
			return false
		}
	}
	return true
}

func conversationMessageText(m provider.Message) string {
	if m.Role == provider.RoleTool && strings.TrimSpace(m.Name) != "" {
		return m.Name + ": " + m.Content
	}
	return m.Content
}

func conversationLocator(path string, index int) string {
	return filepath.Clean(path) + "#" + strconv.Itoa(index)
}

func parseConversationLocator(locator string) (string, int, error) {
	locator = strings.TrimSpace(locator)
	pos := strings.LastIndex(locator, "#")
	if pos <= 0 || pos == len(locator)-1 {
		return "", 0, fmt.Errorf("locator must be formatted as path#index")
	}
	index, err := strconv.Atoi(locator[pos+1:])
	if err != nil || index < 0 {
		return "", 0, fmt.Errorf("invalid locator message index")
	}
	path := filepath.Clean(locator[:pos])
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path, index, nil
}

func conversationPathAllowed(path string, dirs []string) bool {
	path = filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		rel, err := filepath.Rel(dir, path)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func conversationSnippet(text, needle string, max int) string {
	lower := strings.ToLower(text)
	pos := strings.Index(lower, needle)
	if pos < 0 {
		return trimConversationText(text, max)
	}
	start := pos - max/3
	if start < 0 {
		start = 0
	}
	end := start + max
	if end > len(text) {
		end = len(text)
		start = end - max
		if start < 0 {
			start = 0
		}
	}
	out := strings.TrimSpace(text[start:end])
	if start > 0 {
		out = "..." + out
	}
	if end < len(text) {
		out += "..."
	}
	return out
}

func trimConversationText(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max]) + "..."
}

type hostCommand struct{ workDir string }

func (hostCommand) Name() string { return "host_command" }
func (hostCommand) Description() string {
	return "Execute a native host command directly through the operating system shell, bypassing Git Bash argument rewriting. Use as the fallback for OS/system commands when no more specific host tool fits. On Windows, use this for native commands such as shutdown, taskkill, start, sc, reg, and PowerShell. Returns status=done/status=failed style output. This can alter the computer and requires approval unless tool approval is auto/yolo."
}
func (hostCommand) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Command text to execute in the native host shell."},"shell":{"type":"string","enum":["auto","cmd","powershell"],"description":"Windows shell choice. auto uses cmd on Windows and sh elsewhere."},"timeout_seconds":{"type":"integer","minimum":1,"maximum":3600,"description":"Optional timeout in seconds. Defaults to 120."}},"required":["command"]}`)
}
func (hostCommand) ReadOnly() bool { return false }
func (h hostCommand) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command        string `json:"command"`
		Shell          string `json:"shell"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	p.Command = strings.TrimSpace(p.Command)
	if p.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	timeout := time.Duration(p.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := nativeShellArgv(p.Shell, p.Command)
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	proc.HideWindow(cmd)
	cmd.Dir = h.workDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := decodeOutput(buf.Bytes())
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("status=failed tool=host_command shell=%s timeout=%s reason=timeout", normalizedHostShell(p.Shell), timeout)
	}
	if err != nil {
		return out, fmt.Errorf("%s", hostExitSummaryV2(p.Command, err, out, normalizedHostShell(p.Shell)))
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Sprintf("status=done tool=host_command shell=%s result=no_output", normalizedHostShell(p.Shell)), nil
	}
	return fmt.Sprintf("status=done tool=host_command shell=%s\n%s", normalizedHostShell(p.Shell), out), nil
}

func nativeShellArgv(shell, command string) []string {
	if runtime.GOOS == "windows" {
		switch strings.ToLower(strings.TrimSpace(shell)) {
		case "powershell", "pwsh":
			return []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "$OutputEncoding=[Console]::OutputEncoding=[System.Text.Encoding]::UTF8;" + command}
		default:
			return []string{"cmd.exe", "/c", command}
		}
	}
	return []string{"sh", "-c", command}
}

func normalizedHostShell(shell string) string {
	s := strings.ToLower(strings.TrimSpace(shell))
	if runtime.GOOS == "windows" {
		switch s {
		case "powershell", "pwsh":
			return "powershell"
		default:
			return "cmd"
		}
	}
	if s == "" || s == "auto" {
		return "sh"
	}
	return s
}

func hostExitSummary(command string, err error, output string) string {
	code := -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	lowerCmd := strings.ToLower(strings.TrimSpace(command))
	lowerOut := strings.ToLower(output)
	reason := ""
	if strings.HasPrefix(lowerCmd, "shutdown") {
		switch {
		case strings.Contains(lowerOut, "access is denied") || strings.Contains(lowerOut, "鎷掔粷璁块棶"):
			reason = "shutdown failed: current process is not elevated or local policy denied the operation"
		case strings.Contains(lowerOut, "usage:") || strings.Contains(lowerOut, "鐢ㄦ硶"):
			reason = "shutdown failed: arguments were rejected by Windows shutdown.exe"
		default:
			reason = "shutdown failed: Windows rejected the request; check administrator permission, policy, or security software"
		}
	}
	if reason == "" && code >= 0 {
		reason = "process returned non-zero exit code " + strconv.Itoa(code)
	}
	if code >= 0 {
		return fmt.Sprintf("host command exited with code %d: %s", code, reason)
	}
	return fmt.Sprintf("host command failed: %v", err)
}

func hostExitSummaryV2(command string, err error, output string, shell string) string {
	code := -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	lowerCmd := strings.ToLower(strings.TrimSpace(command))
	lowerOut := strings.ToLower(output)
	reason := "process_returned_non_zero"
	advice := "inspect stdout/stderr and adjust the command or use a more specific host tool"
	if strings.HasPrefix(lowerCmd, "shutdown") {
		switch {
		case code == 1190 || strings.Contains(lowerOut, "1190") || (strings.Contains(lowerOut, "already") && strings.Contains(lowerOut, "shutdown")) || strings.Contains(lowerOut, "宸茬粡璁″垝"):
			reason = "shutdown_already_scheduled"
			advice = "a Windows shutdown is already pending; do not repeat the schedule command unless you cancel first"
		case strings.Contains(lowerOut, "access is denied") || strings.Contains(lowerOut, "鎷掔粷璁块棶"):
			reason = "shutdown_access_denied"
			advice = "run with elevated permission or check local policy/security software"
		case strings.Contains(lowerOut, "usage:") || strings.Contains(lowerOut, "鐢ㄦ硶:"):
			reason = "shutdown_arguments_rejected"
			advice = "check shutdown.exe arguments and prefer shell=cmd for Windows-native syntax"
		case strings.Contains(lowerOut, "no shutdown") || strings.Contains(lowerOut, "no pending shutdown") || strings.Contains(lowerOut, "娌℃湁姝ｅ湪鎵ц"):
			reason = "shutdown_none_pending"
			advice = "there is no pending shutdown to cancel"
		default:
			reason = "shutdown_rejected"
			advice = "check administrator permission, local policy, security software, and command arguments"
		}
	}
	if code >= 0 {
		return fmt.Sprintf("status=failed tool=host_command shell=%s exit_code=%d reason=%s advice=%q", shell, code, reason, advice)
	}
	return fmt.Sprintf("status=failed tool=host_command shell=%s reason=execution_failed error=%q advice=%q", shell, err.Error(), advice)
}

type hostSystemInfo struct{ workDir string }

func (hostSystemInfo) Name() string { return "host_system_info" }
func (hostSystemInfo) Description() string {
	return "Return host OS, architecture, current user, working directory, PATH shell hints, and administrator/elevation hint. Read-only."
}
func (hostSystemInfo) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (hostSystemInfo) ReadOnly() bool { return true }
func (h hostSystemInfo) Execute(context.Context, json.RawMessage) (string, error) {
	user := os.Getenv("USERNAME")
	if user == "" {
		user = os.Getenv("USER")
	}
	info := map[string]any{
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		"user":      user,
		"workDir":   h.workDir,
		"path":      os.Getenv("PATH"),
		"elevated":  elevationHint(),
		"timestamp": time.Now().Format(time.RFC3339),
	}
	b, _ := json.MarshalIndent(info, "", "  ")
	return string(b), nil
}

func elevationHint() string {
	if runtime.GOOS != "windows" {
		if os.Geteuid() == 0 {
			return "root"
		}
		return "not-root"
	}
	cmd := exec.Command("cmd.exe", "/c", "net session >nul 2>nul")
	proc.HideWindow(cmd)
	if err := cmd.Run(); err == nil {
		return "administrator"
	}
	return "not-administrator-or-unknown"
}

type hostListProcesses struct{}

func (hostListProcesses) Name() string { return "host_list_processes" }
func (hostListProcesses) Description() string {
	return "List running processes as a compact text table. Read-only."
}
func (hostListProcesses) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"filter":{"type":"string","description":"Optional case-insensitive substring filter."},"limit":{"type":"integer","minimum":1,"maximum":500,"description":"Maximum rows. Defaults to 80."}}}`)
}
func (hostListProcesses) ReadOnly() bool { return true }
func (hostListProcesses) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Filter string `json:"filter"`
		Limit  int    `json:"limit"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 80
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Get-Process | Select-Object Id,ProcessName,CPU,WS | ConvertTo-Csv -NoTypeInformation")
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", "ps -eo pid,comm,%cpu,rss | head -n 500")
	}
	proc.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(decodeOutput(out)), "\n")
	filter := strings.ToLower(strings.TrimSpace(p.Filter))
	var kept []string
	for _, line := range lines {
		if filter == "" || strings.Contains(strings.ToLower(line), filter) || len(kept) == 0 {
			kept = append(kept, line)
			if len(kept) >= p.Limit+1 {
				break
			}
		}
	}
	return strings.Join(kept, "\n"), nil
}

type hostKillProcess struct{}

func (hostKillProcess) Name() string { return "host_kill_process" }
func (hostKillProcess) Description() string {
	return "Terminate a host process by pid or process name. This is destructive and requires approval unless auto/yolo is active."
}
func (hostKillProcess) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pid":{"type":"integer","description":"Process ID to terminate."},"name":{"type":"string","description":"Process name to terminate when pid is not set."},"force":{"type":"boolean","description":"Force termination. Defaults true."}}}`)
}
func (hostKillProcess) ReadOnly() bool { return false }
func (hostKillProcess) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		PID   int    `json:"pid"`
		Name  string `json:"name"`
		Force *bool  `json:"force"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.PID <= 0 && strings.TrimSpace(p.Name) == "" {
		return "", fmt.Errorf("pid or name is required")
	}
	var cmd *exec.Cmd
	force := p.Force == nil || *p.Force
	if runtime.GOOS == "windows" {
		if p.PID > 0 {
			args := []string{"/PID", strconv.Itoa(p.PID)}
			if force {
				args = append([]string{"/F"}, args...)
			}
			cmd = exec.CommandContext(ctx, "taskkill", args...)
		} else {
			args := []string{"/IM", p.Name}
			if force {
				args = append([]string{"/F"}, args...)
			}
			cmd = exec.CommandContext(ctx, "taskkill", args...)
		}
	} else if p.PID > 0 {
		cmd = exec.CommandContext(ctx, "kill", "-TERM", strconv.Itoa(p.PID))
	} else {
		cmd = exec.CommandContext(ctx, "pkill", p.Name)
	}
	proc.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return decodeOutput(out), err
	}
	return firstNonEmpty(decodeOutput(out), "Process termination requested."), nil
}

type hostOpenApp struct{ workDir string }

func (hostOpenApp) Name() string { return "host_open_app" }
func (hostOpenApp) Description() string {
	return "Launch an installed application or executable path. Does not automate the UI after launch."
}
func (hostOpenApp) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"target":{"type":"string","description":"Executable path, document path, URL, or app command to open."},"args":{"type":"array","items":{"type":"string"},"description":"Optional arguments when launching an executable."}},"required":["target"]}`)
}
func (hostOpenApp) ReadOnly() bool { return false }
func (h hostOpenApp) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Target string   `json:"target"`
		Args   []string `json:"args"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Target) == "" {
		return "", fmt.Errorf("target is required")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		if len(p.Args) == 0 {
			cmd = exec.CommandContext(ctx, "cmd.exe", "/c", "start", "", p.Target)
		} else {
			cmd = exec.CommandContext(ctx, p.Target, p.Args...)
		}
	} else if runtime.GOOS == "darwin" && len(p.Args) == 0 {
		cmd = exec.CommandContext(ctx, "open", p.Target)
	} else if len(p.Args) == 0 {
		cmd = exec.CommandContext(ctx, "xdg-open", p.Target)
	} else {
		cmd = exec.CommandContext(ctx, p.Target, p.Args...)
	}
	proc.HideWindow(cmd)
	cmd.Dir = h.workDir
	if err := cmd.Start(); err != nil {
		return "", err
	}
	return "Launch requested.", nil
}

type hostClipboard struct{}

func (hostClipboard) Name() string { return "host_clipboard" }
func (hostClipboard) Description() string {
	return "Read or write plain text clipboard content. Does not support images."
}
func (hostClipboard) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"op":{"type":"string","enum":["read","write"],"description":"Clipboard operation."},"text":{"type":"string","description":"Text to write when op=write."}},"required":["op"]}`)
}
func (hostClipboard) ReadOnly() bool { return false }
func (hostClipboard) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Op   string `json:"op"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	switch strings.ToLower(p.Op) {
	case "read":
		out, err := clipboardRead(ctx)
		if err != nil {
			return "", err
		}
		return out, nil
	case "write":
		if err := clipboardWrite(ctx, p.Text); err != nil {
			return "", err
		}
		return "Clipboard text updated.", nil
	default:
		return "", fmt.Errorf("op must be read or write")
	}
}

func clipboardRead(ctx context.Context) (string, error) {
	if runtime.GOOS == "windows" {
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw")
		proc.HideWindow(cmd)
		out, err := cmd.Output()
		return decodeOutput(out), err
	}
	if runtime.GOOS == "darwin" {
		cmd := exec.CommandContext(ctx, "pbpaste")
		proc.HideWindow(cmd)
		out, err := cmd.Output()
		return decodeOutput(out), err
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", "xclip -selection clipboard -o 2>/dev/null || xsel --clipboard --output 2>/dev/null")
	proc.HideWindow(cmd)
	out, err := cmd.Output()
	return decodeOutput(out), err
}

func clipboardWrite(ctx context.Context, text string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Set-Clipboard -Value $args[0]", text)
		proc.HideWindow(cmd)
		return cmd.Run()
	}
	if runtime.GOOS == "darwin" {
		cmd := exec.CommandContext(ctx, "pbcopy")
		proc.HideWindow(cmd)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", "xclip -selection clipboard 2>/dev/null || xsel --clipboard --input 2>/dev/null")
	proc.HideWindow(cmd)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

type notifyUser struct{}

func (notifyUser) Name() string { return "notify_user" }
func (notifyUser) Description() string {
	return "Show a best-effort desktop notification to the user. Use when a long task, automation, or background job finishes or fails."
}
func (notifyUser) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"}},"required":["body"]}`)
}
func (notifyUser) ReadOnly() bool { return false }
func (notifyUser) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Title == "" {
		p.Title = "O.R.C.A"
	}
	if err := notify.NewPlatformSender().Send(notify.Message{Title: p.Title, Body: p.Body}); err != nil {
		return "", err
	}
	return "Notification sent.", nil
}

type webSearch struct{}

func (webSearch) Name() string { return "web_search" }
func (webSearch) Description() string {
	return "Search the web for current information using China-accessible search sources and return structured title/url/snippet results. Use when you do not already know the URL. Follow up with web_fetch to read a selected result."
}
func (webSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["query"]}`)
}
func (webSearch) ReadOnly() bool { return true }
func (webSearch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Query) == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.Limit <= 0 || p.Limit > 10 {
		p.Limit = 5
	}
	results, searched, err := runWebSearch(ctx, p.Query, p.Limit)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return fmt.Sprintf("No search results parsed from %s. Try a more specific query or use web_fetch with a known URL.", strings.Join(searched, ", ")), nil
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	return string(b), nil
}

type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

type searchSource struct {
	Name  string
	URL   string
	Parse func(string, int) []searchResult
}

func runWebSearch(ctx context.Context, query string, limit int) ([]searchResult, []string, error) {
	sources := []searchSource{
		{Name: "Bing China", URL: "https://cn.bing.com/search?q=" + url.QueryEscape(query), Parse: parseBingHTML},
		{Name: "Baidu", URL: "https://www.baidu.com/s?wd=" + url.QueryEscape(query), Parse: parseBaiduHTML},
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var searched []string
	var errs []string
	for _, source := range sources {
		searched = append(searched, source.Name)
		req, err := http.NewRequestWithContext(ctx, "GET", source.URL, nil)
		if err != nil {
			errs = append(errs, source.Name+": "+err.Error())
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 O.R.C.A")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.6")
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, source.Name+": "+err.Error())
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("%s: HTTP %d", source.Name, resp.StatusCode))
			continue
		}
		if results := source.Parse(string(body), limit); len(results) > 0 {
			return results, searched, nil
		}
		errs = append(errs, source.Name+": no results parsed")
	}
	if len(errs) > 0 {
		return nil, searched, fmt.Errorf("web_search failed after trying %s: %s", strings.Join(searched, ", "), strings.Join(errs, "; "))
	}
	return nil, searched, nil
}

func parseBingHTML(html string, limit int) []searchResult {
	var out []searchResult
	parts := strings.Split(html, `<li class="b_algo"`)
	for _, part := range parts[1:] {
		if len(out) >= limit {
			break
		}
		href := extractBetween(part, `href="`, `"`)
		titleBlock := extractBetween(part, "<h2", "</h2>")
		title := stripHTML(titleBlock)
		snippet := stripHTML(extractBetween(part, `<p>`, `</p>`))
		if title != "" && strings.HasPrefix(href, "http") {
			out = append(out, searchResult{Title: htmlUnescape(title), URL: htmlUnescape(href), Snippet: htmlUnescape(snippet)})
		}
	}
	return out
}

func parseBaiduHTML(html string, limit int) []searchResult {
	var out []searchResult
	parts := strings.Split(html, `class="result`)
	for _, part := range parts[1:] {
		if len(out) >= limit {
			break
		}
		href := extractBetween(part, `href="`, `"`)
		titleBlock := extractBetween(part, `<h3`, `</h3>`)
		if i := strings.Index(titleBlock, ">"); i >= 0 {
			titleBlock = titleBlock[i+1:]
		}
		title := stripHTML(titleBlock)
		snippet := stripHTML(extractBetween(part, `<span class="content-right_8Zs40">`, `</span>`))
		if snippet == "" {
			snippetBlock := extractBetween(part, `<div class="c-abstract`, `</div>`)
			if i := strings.Index(snippetBlock, ">"); i >= 0 {
				snippetBlock = snippetBlock[i+1:]
			}
			snippet = stripHTML(snippetBlock)
		}
		if title != "" && strings.HasPrefix(href, "http") {
			out = append(out, searchResult{Title: htmlUnescape(title), URL: htmlUnescape(href), Snippet: htmlUnescape(snippet)})
		}
	}
	return out
}

type runtimeManager struct {
	workDir string
	mu      sync.Mutex
	node    *replState
	python  *replState
}

type replState struct {
	Vars map[string]string `json:"vars"`
}

func newRuntimeManager(workDir string) *runtimeManager {
	return &runtimeManager{workDir: workDir}
}

type nodeRepl struct{ rt *runtimeManager }

func (nodeRepl) Name() string { return "node_repl_exec" }
func (nodeRepl) Description() string {
	return "Execute JavaScript in a lightweight persistent Node-style REPL. Variables set through the vars object persist across calls. Use for scripts, JSON transforms, package checks, and calculations; not for visual desktop automation."
}
func (nodeRepl) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"},"vars":{"type":"object","additionalProperties":{"type":"string"},"description":"String variables to persist before execution."},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600}},"required":["code"]}`)
}
func (nodeRepl) ReadOnly() bool { return false }
func (n nodeRepl) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Code           string            `json:"code"`
		Vars           map[string]string `json:"vars"`
		TimeoutSeconds int               `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	n.rt.mu.Lock()
	if n.rt.node == nil {
		n.rt.node = &replState{Vars: map[string]string{}}
	}
	for k, v := range p.Vars {
		n.rt.node.Vars[k] = v
	}
	envJSON, _ := json.Marshal(n.rt.node.Vars)
	n.rt.mu.Unlock()
	script := "globalThis.vars = " + string(envJSON) + ";\n" + p.Code + "\nif (globalThis.vars) console.error('__VARS__'+JSON.stringify(globalThis.vars));"
	return runInterpreter(ctx, n.rt.workDir, "node", []string{"-e", script}, p.TimeoutSeconds, func(stderr string) {
		if i := strings.LastIndex(stderr, "__VARS__"); i >= 0 {
			var vars map[string]string
			if json.Unmarshal([]byte(strings.TrimSpace(stderr[i+8:])), &vars) == nil {
				n.rt.mu.Lock()
				n.rt.node.Vars = vars
				n.rt.mu.Unlock()
			}
		}
	})
}

type pythonRepl struct{ rt *runtimeManager }

func (pythonRepl) Name() string { return "python_repl_exec" }
func (pythonRepl) Description() string {
	return "Execute Python in a lightweight persistent REPL. Variables set through vars persist as strings across calls. Use for data processing, document helpers, calculations, and scripts."
}
func (pythonRepl) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"},"vars":{"type":"object","additionalProperties":{"type":"string"}},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600}},"required":["code"]}`)
}
func (pythonRepl) ReadOnly() bool { return false }
func (pyt pythonRepl) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Code           string            `json:"code"`
		Vars           map[string]string `json:"vars"`
		TimeoutSeconds int               `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	pyt.rt.mu.Lock()
	if pyt.rt.python == nil {
		pyt.rt.python = &replState{Vars: map[string]string{}}
	}
	for k, v := range p.Vars {
		pyt.rt.python.Vars[k] = v
	}
	envJSON, _ := json.Marshal(pyt.rt.python.Vars)
	pyt.rt.mu.Unlock()
	script := "import json, sys\nvars = json.loads(" + strconv.Quote(string(envJSON)) + ")\n" + p.Code + "\nprint('__VARS__'+json.dumps(vars), file=sys.stderr)"
	return runInterpreter(ctx, pyt.rt.workDir, pythonExe(), []string{"-c", script}, p.TimeoutSeconds, func(stderr string) {
		if i := strings.LastIndex(stderr, "__VARS__"); i >= 0 {
			var vars map[string]string
			if json.Unmarshal([]byte(strings.TrimSpace(stderr[i+8:])), &vars) == nil {
				pyt.rt.mu.Lock()
				pyt.rt.python.Vars = vars
				pyt.rt.mu.Unlock()
			}
		}
	})
}

func runInterpreter(ctx context.Context, dir, exe string, args []string, timeoutSec int, capture func(stderr string)) (string, error) {
	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, exe, args...)
	proc.HideWindow(cmd)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	errText := decodeOutput(stderr.Bytes())
	if capture != nil {
		capture(errText)
	}
	cleanErr := strings.TrimSpace(stripVarsMarker(errText))
	out := strings.TrimSpace(decodeOutput(stdout.Bytes()))
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("%s timed out after %s", exe, timeout)
	}
	if err != nil {
		if cleanErr != "" {
			return out, fmt.Errorf("%s failed: %s", exe, cleanErr)
		}
		return out, err
	}
	if cleanErr != "" {
		return out + "\nstderr:\n" + cleanErr, nil
	}
	if out == "" {
		return "Execution completed with no output.", nil
	}
	return out, nil
}

func stripVarsMarker(s string) string {
	if i := strings.LastIndex(s, "__VARS__"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func pythonExe() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

type documentInspect struct{ workDir string }

func (documentInspect) Name() string { return "document_inspect" }
func (documentInspect) Description() string {
	return "Inspect a Word/PPT/Excel/PDF or text-like file and return metadata, size, type, and available processing hints."
}
func (documentInspect) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (documentInspect) ReadOnly() bool { return true }
func (d documentInspect) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	path := resolvePath(d.workDir, p.Path)
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	info := map[string]any{
		"path":      path,
		"sizeBytes": st.Size(),
		"modified":  st.ModTime().Format(time.RFC3339),
		"extension": strings.ToLower(filepath.Ext(path)),
		"toolHint":  documentHint(path),
	}
	b, _ := json.MarshalIndent(info, "", "  ")
	return string(b), nil
}

type documentExtract struct{ workDir string }

func (documentExtract) Name() string { return "document_extract" }
func (documentExtract) Description() string {
	return "Extract text from text/markdown/json/csv files and best-effort text from PDF/DOCX/XLSX/PPTX using available local runtimes. For complex editing use python_repl_exec with document libraries."
}
func (documentExtract) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"max_bytes":{"type":"integer","minimum":1024,"maximum":10485760}},"required":["path"]}`)
}
func (documentExtract) ReadOnly() bool { return true }
func (d documentExtract) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path     string `json:"path"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.MaxBytes <= 0 {
		p.MaxBytes = 1 << 20
	}
	path := resolvePath(d.workDir, p.Path)
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".json", ".csv", ".tsv", ".xml", ".html", ".log", ".go", ".ts", ".tsx", ".js", ".py":
		return extractTextFile(path, p.MaxBytes)
	case ".pdf":
		return runDocPython(path, "pdf")
	case ".docx":
		return documentextract.Extract(path, p.MaxBytes)
	case ".xlsx":
		return documentextract.Extract(path, p.MaxBytes)
	case ".pptx":
		return documentextract.Extract(path, p.MaxBytes)
	default:
		return "", fmt.Errorf("unsupported extension %q; use python_repl_exec for custom extraction", ext)
	}
}

func extractTextFile(path string, maxBytes int64) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if int64(len(b)) > maxBytes {
		b = b[:maxBytes]
	}
	return decodeOutput(b), nil
}

func runDocPython(path, kind string) (string, error) {
	code := ""
	switch kind {
	case "pdf":
		code = `import sys
try:
 import PyPDF2
 r=PyPDF2.PdfReader(sys.argv[1])
 print("\n".join((p.extract_text() or "") for p in r.pages))
except Exception as e:
 print("PDF extraction requires PyPDF2 or another PDF library: "+str(e), file=sys.stderr); sys.exit(1)`
	case "docx":
		code = `import sys, zipfile, re
with zipfile.ZipFile(sys.argv[1]) as z:
 data=z.read("word/document.xml").decode("utf-8", "ignore")
 print(re.sub(r"<[^>]+>", " ", data))`
	case "xlsx":
		code = `import sys, zipfile, re
with zipfile.ZipFile(sys.argv[1]) as z:
 names=[n for n in z.namelist() if n.startswith("xl/worksheets/") and n.endswith(".xml")]
 for n in names[:20]:
  print("## "+n)
  print(re.sub(r"<[^>]+>", " ", z.read(n).decode("utf-8","ignore"))[:20000])`
	case "pptx":
		code = `import sys, zipfile, re
with zipfile.ZipFile(sys.argv[1]) as z:
 names=[n for n in z.namelist() if n.startswith("ppt/slides/") and n.endswith(".xml")]
 for n in names:
  print("## "+n)
  print(re.sub(r"<[^>]+>", " ", z.read(n).decode("utf-8","ignore")))`
	}
	cmd := exec.Command(pythonExe(), "-c", code, path)
	proc.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return decodeOutput(out), err
	}
	return decodeOutput(out), nil
}

func documentHint(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		return "Word DOCX: document_extract can read XML text; use python_repl_exec for structured editing."
	case ".pptx":
		return "PowerPoint PPTX: document_extract can read slide XML text; use python_repl_exec for structured editing."
	case ".xlsx":
		return "Excel XLSX: document_extract can read worksheet XML; use python_repl_exec for formulas/tables."
	case ".pdf":
		return "PDF: document_extract tries PyPDF2 when installed."
	default:
		return "Text-like file or unsupported binary."
	}
}

func resolvePath(workDir, p string) string {
	if filepath.IsAbs(p) || workDir == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(workDir, p)
}

func decodeOutput(b []byte) string {
	return strings.ReplaceAll(fileenc.DecodeText(b), "\r\n", "\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func extractBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return ""
	}
	return s[:j]
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func htmlUnescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&#x27;", "'")
	return r.Replace(s)
}

func init() {
	// Keep deterministic tool order in tests that inspect registered names.
	_ = sort.Strings
}
