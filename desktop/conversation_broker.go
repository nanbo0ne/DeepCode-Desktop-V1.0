package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/bot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type ConversationCatalogEntry struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Scope          string `json:"scope"`
	WorkspaceRoot  string `json:"workspaceRoot,omitempty"`
	WorkspaceLabel string `json:"workspaceLabel"`
	Preview        string `json:"preview,omitempty"`
	LastActivityAt int64  `json:"lastActivityAt,omitempty"`
	Running        bool   `json:"running,omitempty"`
}

type DispatchTask struct {
	ID        string `json:"id"`
	TargetID  string `json:"targetId"`
	Status    string `json:"status"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`

	done              chan struct{}
	cancel            context.CancelFunc
	sourceTabID       string
	targetTabID       string
	targetCtrl        *control.Controller
	targetSessionPath string
}

type pendingBrokerPrompt struct {
	taskID            string
	sourceTabID       string
	targetTabID       string
	targetSessionPath string
	ctrl              *control.Controller
	id                string
}

type ConversationBroker struct {
	app *App

	mu               sync.Mutex
	tasks            map[string]*DispatchTask
	targetLocks      map[string]*sync.Mutex
	pendingApprovals map[string]pendingBrokerPrompt
	pendingAnswers   map[string]pendingBrokerPrompt
	rejectedPrompts  map[string]struct{}
	sourceSinks      map[string]sourceSinkRegistration
	nextTask         uint64
	nextSink         uint64
}

type sourceSinkRegistration struct {
	id   uint64
	sink event.Sink
}

func NewConversationBroker(app *App) *ConversationBroker {
	return &ConversationBroker{
		app:              app,
		tasks:            map[string]*DispatchTask{},
		targetLocks:      map[string]*sync.Mutex{},
		pendingApprovals: map[string]pendingBrokerPrompt{},
		pendingAnswers:   map[string]pendingBrokerPrompt{},
		rejectedPrompts:  map[string]struct{}{},
		sourceSinks:      map[string]sourceSinkRegistration{},
	}
}

func (b *ConversationBroker) Tools(sourceTabID, sourceTopicID string) []tool.Tool {
	return []tool.Tool{
		conversationListTool{broker: b, sourceTopicID: sourceTopicID},
		conversationReadTool{broker: b, sourceTopicID: sourceTopicID},
		conversationDispatchTool{broker: b, sourceTabID: sourceTabID, sourceTopicID: sourceTopicID},
		conversationWaitTool{broker: b, sourceTabID: sourceTabID},
		conversationStatusTool{broker: b, sourceTabID: sourceTabID},
		conversationCancelTool{broker: b, sourceTabID: sourceTabID},
		conversationCreateTool{broker: b},
	}
}

func (b *ConversationBroker) Index(sourceTopicID string) string {
	entries := b.Catalog(sourceTopicID)
	if len(entries) == 0 {
		return "Conversation index: no engineering conversations are available."
	}
	var out strings.Builder
	out.WriteString("Conversation index (use conversation tools only when the request needs this context):\n")
	const budget = 12000
	for _, entry := range entries {
		line := fmt.Sprintf("- id=%s | %s | %s | updated=%s", entry.ID, brokerOneLine(entry.Title, 80), entry.WorkspaceLabel, formatCatalogTime(entry.LastActivityAt))
		if entry.Running {
			line += " | running"
		}
		if entry.Preview != "" {
			line += " | " + brokerOneLine(entry.Preview, 140)
		}
		line += "\n"
		if out.Len()+len(line) > budget {
			out.WriteString("- [older conversations omitted; use conversation_list to search]\n")
			break
		}
		out.WriteString(line)
	}
	return strings.TrimSpace(out.String())
}

func (b *ConversationBroker) Catalog(sourceTopicID string) []ConversationCatalogEntry {
	if b == nil || b.app == nil {
		return nil
	}
	f := loadProjectsFile()
	entries := make([]ConversationCatalogEntry, 0, len(f.GlobalTopics)+len(f.Projects)*4)
	appendTopic := func(scope, root, workspaceLabel, topicID string) {
		if topicID == "" || topicID == sourceTopicID || scope == scopeAutomation {
			return
		}
		entry := ConversationCatalogEntry{
			ID:             topicID,
			Title:          topicTitleForTab(scope, root, topicID),
			Scope:          scope,
			WorkspaceRoot:  root,
			WorkspaceLabel: workspaceLabel,
		}
		path := findTopicSession(desktopSessionDir(conversationWorkspaceRoot(scope, root, topicID)), topicID)
		if path != "" {
			if info, ok := sessionInfoForPath(path); ok {
				entry.Preview = info.Preview
				entry.LastActivityAt = info.LastActivityAt.UnixMilli()
			}
		}
		b.app.mu.RLock()
		for _, tab := range b.app.tabs {
			if tab != nil && tab.TopicID == topicID {
				entry.Running = tab.Ctrl != nil && tab.Ctrl.Running()
				if entry.LastActivityAt == 0 {
					entry.LastActivityAt = loadTopicCreatedAt(topicTitleRoot(scope, root), topicID)
				}
				break
			}
		}
		b.app.mu.RUnlock()
		entries = append(entries, entry)
	}
	for _, topicID := range f.GlobalTopics {
		appendTopic("global", "", "Independent Workspace", topicID)
	}
	for _, project := range f.Projects {
		label := projectDisplayName(project)
		for _, topicID := range project.Topics {
			appendTopic("project", project.Root, label, topicID)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Running != entries[j].Running {
			return entries[i].Running
		}
		if entries[i].LastActivityAt != entries[j].LastActivityAt {
			return entries[i].LastActivityAt > entries[j].LastActivityAt
		}
		return entries[i].Title < entries[j].Title
	})
	return entries
}

func conversationWorkspaceRoot(scope, root, topicID string) string {
	if scope == "global" {
		return independentWorkspaceRoot(topicID)
	}
	return root
}

func sessionInfoForPath(path string) (agent.SessionInfo, bool) {
	infos, err := agent.ListSessions(filepath.Dir(path))
	if err != nil {
		return agent.SessionInfo{}, false
	}
	for _, info := range infos {
		if canonicalTabSessionPath(info.Path) == canonicalTabSessionPath(path) {
			return info, true
		}
	}
	return agent.SessionInfo{}, false
}

func formatCatalogTime(ms int64) string {
	if ms <= 0 {
		return "unknown"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04")
}

func (b *ConversationBroker) Read(sourceTopicID, targetID string, limit int) (string, error) {
	entry, ok := b.findCatalogEntry(sourceTopicID, targetID)
	if !ok {
		return "", fmt.Errorf("conversation %q not found or is not dispatchable", targetID)
	}
	path := findTopicSession(desktopSessionDir(conversationWorkspaceRoot(entry.Scope, entry.WorkspaceRoot, entry.ID)), entry.ID)
	if path == "" {
		return fmt.Sprintf("Conversation %s has no saved messages yet.", entry.Title), nil
	}
	session, err := agent.LoadSession(path)
	if err != nil {
		return "", err
	}
	if limit <= 0 || limit > 30 {
		limit = 12
	}
	lines := make([]string, 0, limit)
	for _, message := range session.Snapshot() {
		if message.Role != provider.RoleUser && message.Role != provider.RoleAssistant {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if message.Role == provider.RoleUser {
			content = control.StripComposePrefixes(content)
			if control.IsSyntheticUserMessage(content) {
				continue
			}
		}
		if content == "" {
			continue
		}
		role := "assistant"
		if message.Role == provider.RoleUser {
			role = "user"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", role, brokerOneLine(content, 1200)))
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return fmt.Sprintf("Conversation: %s\nWorkspace: %s\n\n%s", entry.Title, entry.WorkspaceLabel, strings.Join(lines, "\n\n")), nil
}

func (b *ConversationBroker) findCatalogEntry(sourceTopicID, targetID string) (ConversationCatalogEntry, bool) {
	targetID = strings.TrimSpace(targetID)
	for _, entry := range b.Catalog(sourceTopicID) {
		if entry.ID == targetID {
			return entry, true
		}
	}
	return ConversationCatalogEntry{}, false
}

func (b *ConversationBroker) Dispatch(sourceTabID, sourceTopicID, targetID, instruction string) (*DispatchTask, error) {
	done, err := b.app.beginAppWork()
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			done()
		}
	}()
	entry, ok := b.findCatalogEntry(sourceTopicID, targetID)
	if !ok {
		return nil, fmt.Errorf("conversation %q not found or is not dispatchable", targetID)
	}
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return nil, fmt.Errorf("instruction is required")
	}
	tab, err := b.app.ensureBrokerTargetTab(entry)
	if err != nil {
		return nil, err
	}
	b.app.mu.RLock()
	targetCtrl := tab.Ctrl
	targetSessionPath := ""
	if targetCtrl != nil {
		targetSessionPath = targetCtrl.SessionPath()
	}
	b.app.mu.RUnlock()

	b.mu.Lock()
	for _, existing := range b.tasks {
		if existing.sourceTabID == sourceTabID && (existing.Status == "queued" || existing.Status == "running") {
			b.mu.Unlock()
			return nil, fmt.Errorf("this automation conversation already has a dispatched task running; wait or cancel it first")
		}
	}
	b.nextTask++
	now := time.Now().UnixMilli()
	task := &DispatchTask{
		ID:                fmt.Sprintf("dispatch_%d_%d", now, b.nextTask),
		TargetID:          targetID,
		Status:            "queued",
		CreatedAt:         now,
		UpdatedAt:         now,
		done:              make(chan struct{}),
		sourceTabID:       sourceTabID,
		targetTabID:       tab.ID,
		targetCtrl:        targetCtrl,
		targetSessionPath: targetSessionPath,
	}
	b.tasks[task.ID] = task
	lock := b.targetLocks[targetID]
	if lock == nil {
		lock = &sync.Mutex{}
		b.targetLocks[targetID] = lock
	}
	b.mu.Unlock()

	transferred = true
	go func() { defer done(); b.runDispatch(task, tab, lock, instruction) }()
	return cloneDispatchTask(task), nil
}

func (b *ConversationBroker) runDispatch(task *DispatchTask, tab *WorkspaceTab, lock *sync.Mutex, instruction string) {
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	if task.Status == "cancelled" {
		b.mu.Unlock()
		cancel()
		return
	}
	task.cancel = cancel
	task.Status = "running"
	task.UpdatedAt = time.Now().UnixMilli()
	b.mu.Unlock()
	defer cancel()

	ctrl := task.targetCtrl
	if ctrl == nil {
		b.finishTask(task, "", fmt.Errorf("target conversation controller is unavailable"))
		return
	}
	before := len(ctrl.History())
	err := ctrl.RunTurn(ctx, instruction)
	if tab.sink != nil {
		tab.sink.Emit(event.Event{Kind: event.TurnDone, Err: err})
	}
	result := lastAssistantAfter(ctrl.History(), before)
	b.finishTask(task, result, err)
}

func lastAssistantAfter(messages []provider.Message, start int) string {
	if start < 0 || start > len(messages) {
		start = 0
	}
	for i := len(messages) - 1; i >= start; i-- {
		if messages[i].Role == provider.RoleAssistant && strings.TrimSpace(messages[i].Content) != "" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func (b *ConversationBroker) finishTask(task *DispatchTask, result string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if task.Status == "cancelled" {
		b.clearPendingPromptsLocked(task.ID)
		task.cancel = nil
		return
	}
	b.clearPendingPromptsLocked(task.ID)
	if err != nil {
		task.Status = "failed"
		task.Error = err.Error()
	} else {
		task.Status = "completed"
		task.Result = result
	}
	task.UpdatedAt = time.Now().UnixMilli()
	task.cancel = nil
	select {
	case <-task.done:
	default:
		close(task.done)
	}
}

func (b *ConversationBroker) Wait(ctx context.Context, sourceTabID, taskID string, timeout time.Duration) (*DispatchTask, error) {
	b.mu.Lock()
	task := b.tasks[strings.TrimSpace(taskID)]
	if task == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("dispatch task %q not found", taskID)
	}
	if task.sourceTabID != sourceTabID {
		b.mu.Unlock()
		return nil, fmt.Errorf("dispatch task %q does not belong to this automation conversation", taskID)
	}
	done := task.done
	b.mu.Unlock()
	if timeout <= 0 || timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return b.StatusWithError(sourceTabID, taskID, fmt.Errorf("dispatch task is still running"))
	case <-done:
		return b.Status(sourceTabID, taskID)
	}
}

func (b *ConversationBroker) StatusWithError(sourceTabID, taskID string, err error) (*DispatchTask, error) {
	task, statusErr := b.Status(sourceTabID, taskID)
	if statusErr != nil {
		return nil, statusErr
	}
	return task, err
}

func (b *ConversationBroker) Status(sourceTabID, taskID string) (*DispatchTask, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	task := b.tasks[strings.TrimSpace(taskID)]
	if task == nil {
		return nil, fmt.Errorf("dispatch task %q not found", taskID)
	}
	if task.sourceTabID != sourceTabID {
		return nil, fmt.Errorf("dispatch task %q does not belong to this automation conversation", taskID)
	}
	return cloneDispatchTask(task), nil
}

func (b *ConversationBroker) Cancel(sourceTabID, taskID string) (*DispatchTask, error) {
	b.mu.Lock()
	task := b.tasks[strings.TrimSpace(taskID)]
	if task == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("dispatch task %q not found", taskID)
	}
	if task.sourceTabID != sourceTabID {
		b.mu.Unlock()
		return nil, fmt.Errorf("dispatch task %q does not belong to this automation conversation", taskID)
	}
	if task.cancel != nil {
		task.cancel()
	}
	b.clearPendingPromptsLocked(task.ID)
	task.Status = "cancelled"
	task.UpdatedAt = time.Now().UnixMilli()
	select {
	case <-task.done:
	default:
		close(task.done)
	}
	result := cloneDispatchTask(task)
	b.mu.Unlock()
	return result, nil
}

func (b *ConversationBroker) CancelActive(sourceTabID string) bool {
	b.mu.Lock()
	cancelled := false
	for _, task := range b.tasks {
		if task.sourceTabID != sourceTabID || (task.Status != "queued" && task.Status != "running") {
			continue
		}
		if task.cancel != nil {
			task.cancel()
		}
		b.clearPendingPromptsLocked(task.ID)
		task.Status = "cancelled"
		task.UpdatedAt = time.Now().UnixMilli()
		select {
		case <-task.done:
		default:
			close(task.done)
		}
		cancelled = true
	}
	b.mu.Unlock()
	return cancelled
}

func cloneDispatchTask(task *DispatchTask) *DispatchTask {
	if task == nil {
		return nil
	}
	clone := *task
	clone.done = nil
	clone.cancel = nil
	return &clone
}

func (b *ConversationBroker) clearPendingPromptsLocked(taskID string) {
	for id, prompt := range b.pendingApprovals {
		if prompt.taskID == taskID {
			delete(b.pendingApprovals, id)
			b.rejectedPrompts[id] = struct{}{}
		}
	}
	for id, prompt := range b.pendingAnswers {
		if prompt.taskID == taskID {
			delete(b.pendingAnswers, id)
			b.rejectedPrompts[id] = struct{}{}
		}
	}
}

func (b *ConversationBroker) Observe(tabID string, ctrl *control.Controller, e event.Event) {
	if b == nil || ctrl == nil {
		return
	}
	b.mu.Lock()
	sourceTabID := ""
	var sourceSink event.Sink
	var task *DispatchTask
	for _, candidate := range b.tasks {
		if candidate.targetTabID != tabID || candidate.Status != "running" || candidate.targetCtrl != ctrl {
			continue
		}
		if candidate.targetSessionPath != "" && candidate.targetSessionPath != ctrl.SessionPath() {
			continue
		}
		task = candidate
		break
	}
	if task != nil {
		sourceTabID = task.sourceTabID
		sourceSink = b.sourceSinks[sourceTabID].sink
	}
	if task != nil {
		prompt := pendingBrokerPrompt{
			taskID: task.ID, sourceTabID: task.sourceTabID, targetTabID: task.targetTabID,
			targetSessionPath: task.targetSessionPath, ctrl: ctrl,
		}
		switch e.Kind {
		case event.ApprovalRequest:
			prompt.id = e.Approval.ID
			delete(b.rejectedPrompts, prompt.id)
			b.pendingApprovals[e.Approval.ID] = prompt
		case event.AskRequest:
			prompt.id = e.Ask.ID
			delete(b.rejectedPrompts, prompt.id)
			b.pendingAnswers[e.Ask.ID] = prompt
		}
	}
	b.mu.Unlock()
	if sourceSink != nil {
		switch e.Kind {
		case event.ToolDispatch, event.ToolProgress, event.ToolResult, event.ApprovalRequest, event.AskRequest:
			sourceSink.Emit(e)
		}
	}
	if sourceTabID != "" && !strings.HasPrefix(sourceTabID, "bot:") && sourceTabID != tabID && b.app.ctx != nil && (e.Kind == event.ApprovalRequest || e.Kind == event.AskRequest) {
		runtime.EventsEmit(b.app.ctx, eventChannel, toWireTab(e, sourceTabID))
	}
}

func (b *ConversationBroker) RegisterSourceSink(sourceID string, sink event.Sink) func() {
	if b == nil || strings.TrimSpace(sourceID) == "" || sink == nil {
		return func() {}
	}
	b.mu.Lock()
	b.nextSink++
	registrationID := b.nextSink
	b.sourceSinks[sourceID] = sourceSinkRegistration{id: registrationID, sink: sink}
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		if b.sourceSinks[sourceID].id == registrationID {
			delete(b.sourceSinks, sourceID)
		}
		b.mu.Unlock()
	}
}

func (b *ConversationBroker) Approve(id string, allow, session, persist bool) bot.ResponseRouteResult {
	return b.ApproveForSource("", id, allow, session, persist)
}

// ApproveForSource consumes an approval only when the response source still
// owns the live task/controller/session that emitted it. An empty source is
// deliberately rejected so callers cannot bypass source authorization.
func (b *ConversationBroker) ApproveForSource(sourceID, id string, allow, session, persist bool) bot.ResponseRouteResult {
	if b == nil {
		return bot.ResponseNotOwned
	}
	if b.app != nil {
		b.app.mu.RLock()
		defer b.app.mu.RUnlock()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	prompt, owned := b.pendingApprovals[id]
	if !owned {
		if _, stale := b.rejectedPrompts[id]; stale {
			return bot.ResponseRejected
		}
		return bot.ResponseNotOwned
	}
	if !b.promptAuthorizedLocked(prompt, sourceID) || !b.promptTargetIsLiveLocked(prompt) {
		return bot.ResponseRejected
	}
	delete(b.pendingApprovals, id)
	prompt.ctrl.Approve(prompt.id, allow, session, persist)
	return bot.ResponseConsumed
}

func (b *ConversationBroker) Answer(id string, answers []event.AskAnswer) bot.ResponseRouteResult {
	return b.AnswerForSource("", id, answers)
}

// AnswerForSource is the source-authorized counterpart to Answer.
func (b *ConversationBroker) AnswerForSource(sourceID, id string, answers []event.AskAnswer) bot.ResponseRouteResult {
	if b == nil {
		return bot.ResponseNotOwned
	}
	if b.app != nil {
		b.app.mu.RLock()
		defer b.app.mu.RUnlock()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	prompt, owned := b.pendingAnswers[id]
	if !owned {
		if _, stale := b.rejectedPrompts[id]; stale {
			return bot.ResponseRejected
		}
		return bot.ResponseNotOwned
	}
	if !b.promptAuthorizedLocked(prompt, sourceID) || !b.promptTargetIsLiveLocked(prompt) {
		return bot.ResponseRejected
	}
	delete(b.pendingAnswers, id)
	prompt.ctrl.AnswerQuestion(prompt.id, answers)
	return bot.ResponseConsumed
}

func (b *ConversationBroker) promptAuthorizedLocked(prompt pendingBrokerPrompt, sourceID string) bool {
	if strings.TrimSpace(sourceID) == "" || prompt.ctrl == nil || prompt.id == "" || prompt.sourceTabID != sourceID {
		return false
	}
	task := b.tasks[prompt.taskID]
	if task == nil || task.Status != "running" || task.targetTabID != prompt.targetTabID || task.targetCtrl != prompt.ctrl {
		return false
	}
	if prompt.targetSessionPath != "" && prompt.targetSessionPath != prompt.ctrl.SessionPath() {
		return false
	}
	return true
}

// promptTargetIsLiveLocked requires the app read lock when b.app is non-nil;
// callers take it before b.mu to match tabEventSink's lock order.
func (b *ConversationBroker) promptTargetIsLiveLocked(prompt pendingBrokerPrompt) bool {
	if b.app == nil {
		return true
	}
	tab := b.app.tabs[prompt.targetTabID]
	return tab != nil && tab.Ctrl == prompt.ctrl
}

func (a *App) ensureBrokerTargetTab(entry ConversationCatalogEntry) (*WorkspaceTab, error) {
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab != nil && tab.TopicID == entry.ID && tab.Scope != scopeAutomation {
			a.mu.RUnlock()
			return waitForBrokerTabReady(a, tab)
		}
	}
	active := a.activeTabID
	a.mu.RUnlock()
	var meta TabMeta
	var err error
	if entry.Scope == "project" {
		meta, err = a.OpenProjectTab(entry.WorkspaceRoot, entry.ID)
	} else {
		meta, err = a.OpenGlobalTab(entry.ID)
	}
	if active != "" {
		_ = a.SetActiveTab(active)
	}
	if err != nil {
		return nil, err
	}
	a.mu.RLock()
	tab := a.tabs[meta.ID]
	a.mu.RUnlock()
	return waitForBrokerTabReady(a, tab)
}

func waitForBrokerTabReady(a *App, tab *WorkspaceTab) (*WorkspaceTab, error) {
	if tab == nil {
		return nil, fmt.Errorf("target tab was not created")
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.RLock()
		ready, ctrl, startupErr := tab.Ready, tab.Ctrl, tab.StartupErr
		a.mu.RUnlock()
		if ready {
			if ctrl == nil {
				return nil, fmt.Errorf("target conversation failed to start: %s", startupErr)
			}
			return tab, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf("target conversation did not become ready")
}

func brokerOneLine(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit > 0 && len([]rune(value)) > limit {
		runes := []rune(value)
		return string(runes[:limit-1]) + "..."
	}
	return value
}

type conversationListTool struct {
	broker        *ConversationBroker
	sourceTopicID string
}

func (t conversationListTool) Name() string { return "conversation_list" }
func (t conversationListTool) Description() string {
	return "List existing engineering conversations. Use only when the request depends on prior project context and the compact index does not identify one clear target."
}
func (t conversationListTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}}}`)
}
func (t conversationListTool) ReadOnly() bool { return true }
func (t conversationListTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(args, &p)
	entries := t.broker.Catalog(t.sourceTopicID)
	query := strings.ToLower(strings.TrimSpace(p.Query))
	if p.Limit <= 0 || p.Limit > 50 {
		p.Limit = 20
	}
	filtered := make([]ConversationCatalogEntry, 0, p.Limit)
	for _, entry := range entries {
		haystack := strings.ToLower(entry.Title + "\n" + entry.WorkspaceLabel + "\n" + entry.Preview)
		if query != "" && !strings.Contains(haystack, query) {
			continue
		}
		filtered = append(filtered, entry)
		if len(filtered) >= p.Limit {
			break
		}
	}
	b, _ := json.Marshal(filtered)
	return string(b), nil
}

type conversationReadTool struct {
	broker        *ConversationBroker
	sourceTopicID string
}

func (t conversationReadTool) Name() string { return "conversation_read" }
func (t conversationReadTool) Description() string {
	return "Read a bounded recent transcript from one engineering conversation. Call this before claiming to know its contents."
}
func (t conversationReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["conversation_id"],"properties":{"conversation_id":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":30}}}`)
}
func (t conversationReadTool) ReadOnly() bool { return true }
func (t conversationReadTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID    string `json:"conversation_id"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	return t.broker.Read(t.sourceTopicID, p.ID, p.Limit)
}

type conversationDispatchTool struct {
	broker                     *ConversationBroker
	sourceTabID, sourceTopicID string
}

func (t conversationDispatchTool) Name() string { return "conversation_dispatch" }
func (t conversationDispatchTool) Description() string {
	return "Send a task to one existing engineering conversation. Returns a task id immediately; call conversation_wait when the answer depends on the result."
}
func (t conversationDispatchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["conversation_id","instruction"],"properties":{"conversation_id":{"type":"string"},"instruction":{"type":"string"}}}`)
}
func (t conversationDispatchTool) ReadOnly() bool { return false }
func (t conversationDispatchTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID          string `json:"conversation_id"`
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	task, err := t.broker.Dispatch(t.sourceTabID, t.sourceTopicID, p.ID, p.Instruction)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(task)
	return string(b), nil
}

type conversationWaitTool struct {
	broker      *ConversationBroker
	sourceTabID string
}

func (t conversationWaitTool) Name() string { return "conversation_wait" }
func (t conversationWaitTool) Description() string {
	return "Wait for a dispatched conversation task and return its real final result. Use this before answering when the result is required."
}
func (t conversationWaitTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600}}}`)
}
func (t conversationWaitTool) ReadOnly() bool { return true }
func (t conversationWaitTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID      string `json:"task_id"`
		Timeout int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	task, err := t.broker.Wait(ctx, t.sourceTabID, p.ID, time.Duration(p.Timeout)*time.Second)
	b, _ := json.Marshal(task)
	if err != nil {
		return string(b), err
	}
	return string(b), nil
}

type conversationStatusTool struct {
	broker      *ConversationBroker
	sourceTabID string
}

func (t conversationStatusTool) Name() string { return "conversation_status" }
func (t conversationStatusTool) Description() string {
	return "Check the current status of a dispatched conversation task without waiting."
}
func (t conversationStatusTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string"}}}`)
}
func (t conversationStatusTool) ReadOnly() bool { return true }
func (t conversationStatusTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"task_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	task, err := t.broker.Status(t.sourceTabID, p.ID)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(task)
	return string(b), nil
}

type conversationCancelTool struct {
	broker      *ConversationBroker
	sourceTabID string
}

func (t conversationCancelTool) Name() string { return "conversation_cancel" }
func (t conversationCancelTool) Description() string {
	return "Cancel a conversation task dispatched by this automation workspace."
}
func (t conversationCancelTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string"}}}`)
}
func (t conversationCancelTool) ReadOnly() bool { return false }
func (t conversationCancelTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"task_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	task, err := t.broker.Cancel(t.sourceTabID, p.ID)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(task)
	return string(b), nil
}

type conversationCreateTool struct{ broker *ConversationBroker }

func (t conversationCreateTool) Name() string { return "conversation_create" }
func (t conversationCreateTool) Description() string {
	return "Create a new ordinary engineering conversation in the independent workspace or an existing project. This never creates another automation conversation."
}
func (t conversationCreateTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["global","project"]},"workspace_root":{"type":"string"},"title":{"type":"string"}}}`)
}
func (t conversationCreateTool) ReadOnly() bool { return false }
func (t conversationCreateTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Scope string `json:"scope"`
		Root  string `json:"workspace_root"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Scope == "" {
		p.Scope = "global"
	}
	if p.Scope != "global" && p.Scope != "project" {
		return "", fmt.Errorf("scope must be global or project")
	}
	if p.Scope == "project" && strings.TrimSpace(p.Root) == "" {
		return "", fmt.Errorf("workspace_root is required for project scope")
	}
	topic, err := t.broker.app.CreateTopic(p.Scope, p.Root, p.Title)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(topic)
	return string(b), nil
}
