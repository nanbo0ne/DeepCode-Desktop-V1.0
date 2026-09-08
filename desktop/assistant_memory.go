package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/memory"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type AssistantMemorySettings struct {
	AssistantAutoMemoryEnabled   bool `json:"assistantAutoMemoryEnabled"`
	AssistantMemoryRecallEnabled bool `json:"assistantMemoryRecallEnabled"`
}

type assistantMemoryPendingFile struct {
	Items map[string]assistantMemoryPendingItem `json:"items"`
}

type assistantMemoryPendingItem struct {
	SessionPath           string `json:"sessionPath"`
	TopicID               string `json:"topicID"`
	WorkspaceRoot         string `json:"workspaceRoot"`
	Model                 string `json:"model,omitempty"`
	LastProcessedMessages int    `json:"lastProcessedMessages"`
	LastQueuedMessages    int    `json:"lastQueuedMessages,omitempty"`
	MarkedAt              int64  `json:"markedAt"`
	LastAttemptAt         int64  `json:"lastAttemptAt,omitempty"`
	LastErrorAt           int64  `json:"lastErrorAt,omitempty"`
	RetryCount            int    `json:"retryCount,omitempty"`
	Status                string `json:"status"`
	Error                 string `json:"error,omitempty"`
}

type assistantMemoryCandidate struct {
	SessionPath   string
	TopicID       string
	WorkspaceRoot string
	PromptMode    string
	Model         string
}

type assistantMemoryUpdate struct {
	Action      string  `json:"action"`
	Name        string  `json:"name"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	Body        string  `json:"body"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

type assistantMemoryResponse struct {
	Action      string                  `json:"action"`
	Name        string                  `json:"name"`
	Title       string                  `json:"title"`
	Description string                  `json:"description"`
	Type        string                  `json:"type"`
	Body        string                  `json:"body"`
	Confidence  float64                 `json:"confidence"`
	Reason      string                  `json:"reason"`
	Memories    []assistantMemoryUpdate `json:"memories"`
}

func (a *App) GetAssistantMemorySettings() (AssistantMemorySettings, error) {
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return AssistantMemorySettings{AssistantAutoMemoryEnabled: true, AssistantMemoryRecallEnabled: true}, err
	}
	return AssistantMemorySettings{
		AssistantAutoMemoryEnabled:   cfg.DesktopAssistantAutoMemoryEnabled(),
		AssistantMemoryRecallEnabled: cfg.DesktopAssistantMemoryRecallEnabled(),
	}, nil
}

func (a *App) SetAssistantMemorySettings(settings AssistantMemorySettings) error {
	if err := a.applyConfigOnly(func(c *config.Config) error {
		if err := c.SetDesktopAssistantAutoMemory(settings.AssistantAutoMemoryEnabled); err != nil {
			return err
		}
		return c.SetDesktopAssistantMemoryRecall(settings.AssistantMemoryRecallEnabled)
	}); err != nil {
		return err
	}
	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, tab := range a.tabs {
		tabs = append(tabs, tab)
	}
	a.mu.RUnlock()
	for _, tab := range tabs {
		if tab != nil && usesAssistantMemory(currentTabPromptMode(tab)) && tab.Ctrl != nil {
			tab.Ctrl.SetMemoryReminder(settings.AssistantMemoryRecallEnabled)
		}
	}
	return nil
}

func (a *App) ClearAssistantMemories() error {
	if err := memory.ClearAssistantStores(config.MemoryUserDir()); err != nil {
		return err
	}
	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, tab := range a.tabs {
		tabs = append(tabs, tab)
	}
	a.mu.RUnlock()
	for _, tab := range tabs {
		if tab != nil && tab.Ctrl != nil {
			tab.Ctrl.RefreshMemory()
		}
	}
	return nil
}

func assistantMemoryStatePath() string {
	return filepath.Join(desktopConfigDir(), "assistant-memory-pending.json")
}

func loadAssistantMemoryPendingFile() assistantMemoryPendingFile {
	out := assistantMemoryPendingFile{Items: map[string]assistantMemoryPendingItem{}}
	b, err := os.ReadFile(assistantMemoryStatePath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	if out.Items == nil {
		out.Items = map[string]assistantMemoryPendingItem{}
	}
	return out
}

func saveAssistantMemoryPendingFile(f assistantMemoryPendingFile) error {
	if f.Items == nil {
		f.Items = map[string]assistantMemoryPendingItem{}
	}
	path := assistantMemoryStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (a *App) assistantMemoryCandidateForTabLocked(tab *WorkspaceTab) assistantMemoryCandidate {
	if tab == nil {
		return assistantMemoryCandidate{}
	}
	path := strings.TrimSpace(tab.SessionPath)
	if path == "" && tab.Ctrl != nil {
		path = strings.TrimSpace(tab.Ctrl.SessionPath())
	}
	return assistantMemoryCandidate{
		SessionPath:   path,
		TopicID:       strings.TrimSpace(tab.TopicID),
		WorkspaceRoot: strings.TrimSpace(tab.WorkspaceRoot),
		PromptMode:    currentTabPromptMode(tab),
		Model:         strings.TrimSpace(tab.model),
	}
}

func (a *App) markAssistantMemoryPendingForCandidate(c assistantMemoryCandidate, runNow bool) {
	if !assistantMemoryFeatureAvailable(c.WorkspaceRoot) {
		return
	}
	if !usesAssistantMemory(c.PromptMode) || strings.TrimSpace(c.SessionPath) == "" {
		return
	}
	cfg, err := config.LoadForRoot(c.WorkspaceRoot)
	if err != nil || !cfg.DesktopAssistantAutoMemoryEnabled() {
		return
	}
	_, totalMessages, _ := assistantMemoryMessagesSince(c.SessionPath, 0)
	a.assistantMemoryMu.Lock()
	f := loadAssistantMemoryPendingFile()
	key := canonicalTabSessionPath(c.SessionPath)
	item := f.Items[key]
	if item.LastProcessedMessages < 0 {
		item.LastProcessedMessages = 0
	}
	if (item.Status == "failed" || item.Status == "ignored") && totalMessages <= item.LastQueuedMessages {
		status := item.Status
		f.Items[key] = item
		_ = saveAssistantMemoryPendingFile(f)
		a.assistantMemoryMu.Unlock()
		if runNow && status == "failed" {
			a.schedulePendingAssistantMemories()
		}
		return
	}
	item.SessionPath = key
	item.TopicID = c.TopicID
	item.WorkspaceRoot = c.WorkspaceRoot
	item.Model = strings.TrimSpace(c.Model)
	item.LastQueuedMessages = totalMessages
	item.MarkedAt = time.Now().UnixMilli()
	item.Status = "pending"
	item.LastAttemptAt = 0
	item.LastErrorAt = 0
	item.RetryCount = 0
	item.Error = ""
	f.Items[key] = item
	_ = saveAssistantMemoryPendingFile(f)
	a.assistantMemoryMu.Unlock()
	if runNow {
		a.schedulePendingAssistantMemories()
	}
}

func (a *App) markActiveAssistantMemoryPending(runNow bool) {
	a.mu.RLock()
	c := a.assistantMemoryCandidateForTabLocked(a.activeTabLocked())
	a.mu.RUnlock()
	a.markAssistantMemoryPendingForCandidate(c, runNow)
}

const assistantMemoryIdleDelay = 8 * time.Second

func (a *App) schedulePendingAssistantMemories() {
	if !assistantMemoryFeatureAvailable("") {
		return
	}
	a.assistantMemoryMu.Lock()
	if a.assistantMemoryTimer != nil {
		a.assistantMemoryTimer.Stop()
	}
	a.assistantMemoryTimer = time.AfterFunc(assistantMemoryIdleDelay, func() {
		a.startPendingAssistantMemoriesIfIdle()
	})
	a.assistantMemoryMu.Unlock()
}

func (a *App) startPendingAssistantMemoriesIfIdle() {
	if !assistantMemoryFeatureAvailable("") {
		return
	}
	a.assistantMemoryMu.Lock()
	if a.assistantMemoryWorkerRunning {
		a.assistantMemoryMu.Unlock()
		return
	}
	if a.anyTabRunning() {
		a.assistantMemoryMu.Unlock()
		a.schedulePendingAssistantMemories()
		return
	}
	a.assistantMemoryWorkerRunning = true
	a.assistantMemoryMu.Unlock()
	go func() {
		shouldReschedule := false
		defer func() {
			a.assistantMemoryMu.Lock()
			a.assistantMemoryWorkerRunning = false
			a.assistantMemoryMu.Unlock()
			if shouldReschedule || a.hasRunnableAssistantMemoryPending() {
				a.schedulePendingAssistantMemories()
			}
		}()
		shouldReschedule = a.processPendingAssistantMemories()
	}()
}

func (a *App) anyTabRunning() bool {
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil && tab.Ctrl.Running() {
			a.mu.RUnlock()
			return true
		}
	}
	gw := a.botGateway
	a.mu.RUnlock()
	return gw != nil && gw.ActiveCount() > 0
}

func (a *App) hasRunnableAssistantMemoryPending() bool {
	a.assistantMemoryMu.Lock()
	defer a.assistantMemoryMu.Unlock()
	f := loadAssistantMemoryPendingFile()
	now := time.Now()
	for _, item := range f.Items {
		if item.Status == "pending" || (item.Status == "failed" && assistantMemoryShouldRetry(item, now)) {
			return true
		}
	}
	return false
}

func (a *App) processPendingAssistantMemories() bool {
	done, err := a.beginAppWork()
	if err != nil {
		return true
	}
	defer done()
	for {
		if a.anyTabRunning() {
			return true
		}
		key, item, ok := a.claimNextAssistantMemoryPending()
		if !ok {
			return false
		}
		err := a.processAssistantMemoryItem(&item)
		a.finishAssistantMemoryPending(key, item, err)
	}
}

func (a *App) claimNextAssistantMemoryPending() (string, assistantMemoryPendingItem, bool) {
	a.assistantMemoryMu.Lock()
	defer a.assistantMemoryMu.Unlock()
	f := loadAssistantMemoryPendingFile()
	now := time.Now()
	for key, item := range f.Items {
		if item.Status != "pending" && item.Status != "failed" {
			continue
		}
		if item.Status == "failed" && !assistantMemoryShouldRetry(item, now) {
			continue
		}
		item.Status = "running"
		item.LastAttemptAt = now.UnixMilli()
		item.Error = ""
		f.Items[key] = item
		_ = saveAssistantMemoryPendingFile(f)
		return key, item, true
	}
	return "", assistantMemoryPendingItem{}, false
}

func (a *App) finishAssistantMemoryPending(key string, item assistantMemoryPendingItem, err error) {
	a.assistantMemoryMu.Lock()
	defer a.assistantMemoryMu.Unlock()
	f := loadAssistantMemoryPendingFile()
	current := f.Items[key]
	// If a newer tab switch marked the same session while generation was running,
	// keep its pending state so the new messages are processed by a later pass.
	if current.Status == "pending" && current.MarkedAt > item.MarkedAt {
		if item.LastProcessedMessages > current.LastProcessedMessages {
			current.LastProcessedMessages = item.LastProcessedMessages
			f.Items[key] = current
			_ = saveAssistantMemoryPendingFile(f)
		}
		return
	}
	if err != nil {
		item.Status = "failed"
		item.Error = err.Error()
		item.RetryCount++
		item.LastErrorAt = time.Now().UnixMilli()
		if item.RetryCount >= assistantMemoryMaxRetries {
			item.Status = "ignored"
		}
	} else {
		item.Status = "done"
		item.Error = ""
		item.RetryCount = 0
		item.LastErrorAt = 0
	}
	f.Items[key] = item
	_ = saveAssistantMemoryPendingFile(f)
}

const assistantMemoryMaxRetries = 5

func assistantMemoryRetryDelay(retryCount int) time.Duration {
	if retryCount <= 0 {
		return 0
	}
	delays := []time.Duration{
		10 * time.Minute,
		time.Hour,
		6 * time.Hour,
		24 * time.Hour,
		24 * time.Hour,
	}
	if retryCount > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[retryCount-1]
}

func assistantMemoryShouldRetry(item assistantMemoryPendingItem, now time.Time) bool {
	if item.RetryCount >= assistantMemoryMaxRetries {
		return false
	}
	if item.LastErrorAt <= 0 {
		return true
	}
	return now.UnixMilli()-item.LastErrorAt >= assistantMemoryRetryDelay(item.RetryCount).Milliseconds()
}

func (a *App) processAssistantMemoryItem(item *assistantMemoryPendingItem) error {
	sessionPath := strings.TrimSpace(item.SessionPath)
	if sessionPath == "" {
		return nil
	}
	msgs, total, err := assistantMemoryMessagesSince(sessionPath, item.LastProcessedMessages)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		item.LastProcessedMessages = total
		return nil
	}
	updates, err := a.generateAssistantMemoryUpdates(*item, msgs)
	if err != nil {
		return err
	}
	store := memory.AssistantStoreFor(config.MemoryUserDir(), item.WorkspaceRoot)
	if isAutomationWorkspaceRoot(item.WorkspaceRoot) {
		if canonical, err := memory.EnsureCanonicalAssistantStore(config.MemoryUserDir()); err == nil {
			store = canonical
		}
	}
	now := memory.NowRFC3339()
	for _, update := range updates {
		if !assistantMemoryUpdateAllowed(update) {
			continue
		}
		name := update.Name
		if name == "" {
			name = update.Title
		}
		if name == "" {
			name = update.Description
		}
		switch strings.ToLower(strings.TrimSpace(update.Action)) {
		case "forget":
			if name != "" {
				_ = store.Delete(name)
			}
		case "create", "update":
			existing := assistantMemoryFindSimilar(store, update)
			if existing.Name != "" {
				name = existing.Name
			}
			createdAt := now
			if existing.CreatedAt != "" {
				createdAt = existing.CreatedAt
			}
			_, err := store.Save(memory.Memory{
				Name:           name,
				Title:          update.Title,
				Description:    update.Description,
				Type:           memory.NormalizeType(update.Type),
				Body:           update.Body,
				Source:         "auto",
				CreatedAt:      createdAt,
				UpdatedAt:      now,
				Confidence:     update.Confidence,
				LastEvidenceAt: now,
			})
			if err != nil {
				return err
			}
		}
	}
	item.LastProcessedMessages = total
	return nil
}

func assistantMemoryMessagesSince(sessionPath string, processed int) ([]provider.Message, int, error) {
	s, err := agent.LoadSession(sessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, processed, nil
		}
		return nil, processed, err
	}
	var real []provider.Message
	for _, m := range s.Snapshot() {
		if m.Role == provider.RoleUser {
			m.Content = control.StripComposePrefixes(m.Content)
		}
		if !assistantMemoryRealMessage(m) {
			continue
		}
		real = append(real, provider.Message{Role: m.Role, Content: strings.TrimSpace(m.Content)})
	}
	if processed < 0 {
		processed = 0
	}
	if processed > len(real) {
		processed = len(real)
	}
	return real[processed:], len(real), nil
}

func assistantMemoryRealMessage(m provider.Message) bool {
	content := strings.TrimSpace(m.Content)
	if content == "" {
		return false
	}
	if m.Role != provider.RoleUser && m.Role != provider.RoleAssistant {
		return false
	}
	if strings.HasPrefix(content, "<system-reminder>") ||
		strings.HasPrefix(content, "<workflow-reminder>") ||
		strings.HasPrefix(content, "<context-checkpoint>") ||
		strings.HasPrefix(content, "<memory-update>") ||
		strings.HasPrefix(content, "<background-jobs>") ||
		control.IsSyntheticUserMessage(content) {
		return false
	}
	return true
}

func assistantMemoryFeatureAvailable(workspaceRoot string) bool {
	if assistantMemoryAvailable() || isAutomationWorkspaceRoot(workspaceRoot) {
		return true
	}
	return strings.TrimSpace(workspaceRoot) == "" && strings.TrimSpace(automationWorkspaceRoot()) != ""
}

func isAutomationWorkspaceRoot(root string) bool {
	root = strings.TrimSpace(root)
	automationRoot := strings.TrimSpace(automationWorkspaceRoot())
	if root == "" || automationRoot == "" {
		return false
	}
	a, errA := filepath.Abs(root)
	b, errB := filepath.Abs(automationRoot)
	return errA == nil && errB == nil && filepath.Clean(a) == filepath.Clean(b)
}

func (a *App) generateAssistantMemoryUpdates(item assistantMemoryPendingItem, msgs []provider.Message) ([]assistantMemoryUpdate, error) {
	entry, err := a.providerEntryForWorkspace(item.WorkspaceRoot, item.Model)
	if err != nil {
		return nil, err
	}
	prov, err := boot.NewProviderWithProxy(entry, netclient.ProxySpec{Mode: netclient.ModeAuto})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 45*time.Second)
	defer cancel()
	req := provider.Request{
		Temperature: 0,
		MaxTokens:   1200,
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: assistantMemoryUpdateSystemPrompt},
			{Role: provider.RoleUser, Content: assistantMemoryUpdateUserPrompt(item, msgs)},
		},
	}
	ch, err := prov.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			b.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err != nil {
				return nil, chunk.Err
			}
		}
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return nil, err
	}
	return parseAssistantMemoryUpdates(b.String())
}

func (a *App) providerEntryForWorkspace(workspaceRoot string, modelRef string) (*config.ProviderEntry, error) {
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(modelRef)
	if ref == "" {
		ref = cfg.DefaultModel
	}
	resolved, _, ok := cfg.ResolveModelWithFallback(ref)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", ref)
	}
	entry, ok := cfg.ResolveModel(resolved)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", resolved)
	}
	return entry, nil
}

const assistantMemoryUpdateSystemPrompt = `You update Orca's private assistant-mode profile memory.

Return strict JSON only. Do not use tools. Do not include Markdown fences.

Output schema:
{"memories":[{"action":"none|create|update|forget","name":"kebab-case","title":"short label","description":"one-line future-use hook","type":"user|feedback|project|reference","body":"short Markdown memory focused on how to help later","confidence":0.0,"reason":"brief evidence"}]}

Remember only durable, useful personalization: stable preferences, communication style, language preference, interests, working/learning style, ongoing projects or goals, repeated problem patterns, and explicit "remember this" requests.

Do not remember one-off questions, short-term plans, transient emotions, ordinary chat flow, full conversation content, passwords, API keys, account identifiers, payment details, precise addresses, government IDs, medical/health profiles, political/religious identity, or other sensitive personal profiling.

If the user says not to remember something, or asks to forget something, return a forget action when it maps to an existing memory, otherwise none.

Use confidence >= 0.75 only when the memory is clearly durable and helpful for future conversations. Prefer none over speculative profile building.`

func assistantMemoryUpdateUserPrompt(item assistantMemoryPendingItem, msgs []provider.Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Conversation title/topic ID: %s\n", item.TopicID)
	fmt.Fprintf(&b, "Workspace: %s\n", item.WorkspaceRoot)
	b.WriteString("\nExisting assistant memory index:\n")
	store := memory.AssistantStoreFor(config.MemoryUserDir(), item.WorkspaceRoot)
	if isAutomationWorkspaceRoot(item.WorkspaceRoot) {
		if canonical, err := memory.EnsureCanonicalAssistantStore(config.MemoryUserDir()); err == nil {
			store = canonical
		}
	}
	b.WriteString(strings.TrimSpace(store.Index()))
	b.WriteString("\n\nNew real user/assistant messages since the last memory update:\n")
	for _, m := range msgs {
		role := "assistant"
		if m.Role == provider.RoleUser {
			role = "user"
		}
		content := strings.TrimSpace(m.Content)
		if len(content) > 4000 {
			content = content[:4000] + "\n[truncated]"
		}
		fmt.Fprintf(&b, "\n<message role=%q>\n%s\n</message>\n", role, content)
	}
	return b.String()
}

func parseAssistantMemoryUpdates(text string) ([]assistantMemoryUpdate, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	var resp assistantMemoryResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		var one assistantMemoryUpdate
		if err2 := json.Unmarshal([]byte(text), &one); err2 != nil {
			return nil, err
		}
		resp.Memories = []assistantMemoryUpdate{one}
	}
	if len(resp.Memories) == 0 && resp.Action != "" {
		resp.Memories = []assistantMemoryUpdate{{
			Action: resp.Action, Name: resp.Name, Title: resp.Title, Description: resp.Description,
			Type: resp.Type, Body: resp.Body, Confidence: resp.Confidence, Reason: resp.Reason,
		}}
	}
	return resp.Memories, nil
}

var sensitiveAssistantMemoryRe = regexp.MustCompile(`(?i)(password|passcode|api[_ -]?key|secret|token|credential|private[_ -]?key|身份证|护照|银行卡|信用卡|支付|密码|密钥|住址|家庭住址|medical|diagnosis|religion|political)`)

func assistantMemoryUpdateAllowed(update assistantMemoryUpdate) bool {
	action := strings.ToLower(strings.TrimSpace(update.Action))
	if action == "" || action == "none" {
		return false
	}
	if action == "forget" {
		return strings.TrimSpace(update.Name) != "" || strings.TrimSpace(update.Description) != ""
	}
	if action != "create" && action != "update" {
		return false
	}
	if update.Confidence < 0.75 {
		return false
	}
	if strings.TrimSpace(update.Description) == "" || strings.TrimSpace(update.Body) == "" {
		return false
	}
	combined := update.Title + "\n" + update.Description + "\n" + update.Body
	return !sensitiveAssistantMemoryRe.MatchString(combined)
}

func assistantMemoryFindSimilar(store memory.Store, update assistantMemoryUpdate) memory.Memory {
	target := strings.ToLower(strings.TrimSpace(update.Name))
	if target != "" && store.Exists(target) {
		for _, m := range store.List() {
			if strings.EqualFold(m.Name, target) {
				return m
			}
		}
	}
	desc := strings.ToLower(strings.TrimSpace(update.Description))
	title := strings.ToLower(strings.TrimSpace(update.Title))
	for _, m := range store.List() {
		if title != "" && strings.EqualFold(strings.TrimSpace(m.Title), strings.TrimSpace(update.Title)) {
			return m
		}
		if desc != "" && strings.EqualFold(strings.TrimSpace(m.Description), strings.TrimSpace(update.Description)) {
			return m
		}
	}
	return memory.Memory{}
}
