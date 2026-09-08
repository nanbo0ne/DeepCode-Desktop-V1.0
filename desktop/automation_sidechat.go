package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool/hosttools"
)

type AutomationView = hosttools.AutomationView

func (a *App) ListAutomations() []AutomationView {
	return hosttools.ListAutomations()
}

func (a *App) PauseAutomation(id string) error {
	if hosttools.PauseAutomation(id) {
		return nil
	}
	return fmt.Errorf("automation %q is not scheduled or does not exist", id)
}

func (a *App) ResumeAutomation(id string) error {
	done, err := a.beginAppWork()
	if err != nil {
		return err
	}
	defer done()
	if hosttools.ResumeAutomation(id) {
		return nil
	}
	return fmt.Errorf("automation %q is not paused or does not exist", id)
}

func (a *App) CancelAutomation(id string) error {
	if hosttools.CancelAutomation(id) {
		return nil
	}
	return fmt.Errorf("automation %q does not exist", id)
}

func (a *App) ClearFinishedAutomations() error {
	hosttools.ClearFinishedAutomations()
	return nil
}

type SideChatMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"createdAt"`
}

type sideChatCancel struct {
	token  string
	cancel context.CancelFunc
}

type sideChatFile struct {
	Sessions map[string][]SideChatMessage `json:"sessions"`
}

func sideChatStorePath() string {
	return filepath.Join(desktopConfigDir(), "side-chats.json")
}

func loadSideChatFile() sideChatFile {
	out := sideChatFile{Sessions: map[string][]SideChatMessage{}}
	b, err := os.ReadFile(sideChatStorePath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	if out.Sessions == nil {
		out.Sessions = map[string][]SideChatMessage{}
	}
	return out
}

func saveSideChatFile(f sideChatFile) error {
	path := sideChatStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (a *App) sideChatKey(tabID string) string {
	tab := a.tabByID(tabID)
	if tab == nil {
		return strings.TrimSpace(tabID)
	}
	if tab.Ctrl != nil {
		if path := strings.TrimSpace(tab.Ctrl.SessionPath()); path != "" {
			return canonicalTabSessionPath(path)
		}
	}
	if path := strings.TrimSpace(tab.currentSessionPath()); path != "" {
		return canonicalTabSessionPath(path)
	}
	return tab.ID
}

func (a *App) ListSideChat(tabID string) []SideChatMessage {
	key := a.sideChatKey(tabID)
	if key == "" {
		return []SideChatMessage{}
	}
	f := loadSideChatFile()
	history := f.Sessions[key]
	if len(history) == 0 {
		return []SideChatMessage{}
	}
	return append([]SideChatMessage{}, history...)
}

func (a *App) ClearSideChat(tabID string) error {
	key := a.sideChatKey(tabID)
	if key == "" {
		return nil
	}
	f := loadSideChatFile()
	delete(f.Sessions, key)
	return saveSideChatFile(f)
}

func (a *App) CancelSideChat(tabID string) error {
	key := a.sideChatKey(tabID)
	a.sideChatMu.Lock()
	cancel := a.sideChatCancels[key].cancel
	delete(a.sideChatCancels, key)
	a.sideChatMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *App) SendSideChat(tabID, input string) error {
	done, err := a.beginAppWork()
	if err != nil {
		return err
	}
	defer done()
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	key := a.sideChatKey(tabID)
	if key == "" {
		return fmt.Errorf("side chat session is not ready")
	}
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		return err
	}
	p, err := boot.NewProvider(entry)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(a.bootContext())
	token := newSideChatID("run")
	a.sideChatMu.Lock()
	if prev := a.sideChatCancels[key]; prev.cancel != nil {
		prev.cancel()
	}
	a.sideChatCancels[key] = sideChatCancel{token: token, cancel: cancel}
	a.sideChatMu.Unlock()
	defer func() {
		cancel()
		a.sideChatMu.Lock()
		if a.sideChatCancels[key].token == token {
			delete(a.sideChatCancels, key)
		}
		a.sideChatMu.Unlock()
	}()

	f := loadSideChatFile()
	history := append([]SideChatMessage(nil), f.Sessions[key]...)
	userMsg := SideChatMessage{ID: newSideChatID("user"), Role: "user", Content: input, CreatedAt: time.Now().UnixMilli()}
	history = append(history, userMsg)
	req := provider.Request{
		Messages:    a.sideChatRequestMessages(tabID, history),
		Temperature: 0.2,
		MaxTokens:   1200,
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return err
	}
	var answer strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			answer.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err != nil {
				return chunk.Err
			}
		case provider.ChunkDone, provider.ChunkUsage:
		}
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	text := strings.TrimSpace(answer.String())
	if text == "" {
		text = "我没有拿到有效回复。"
	}
	history = append(history, SideChatMessage{ID: newSideChatID("assistant"), Role: "assistant", Content: text, CreatedAt: time.Now().UnixMilli()})
	f.Sessions[key] = trimSideChatHistory(history)
	return saveSideChatFile(f)
}

func (a *App) sideChatRequestMessages(tabID string, side []SideChatMessage) []provider.Message {
	contextText := a.sideChatMainContext(tabID)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "你是 Orca 的侧边聊天助手。你只能根据主对话和用户问题进行只读解答，不要修改文件、运行命令、创建自动化或调用任何工具。回答要简洁、具体、中文优先。主对话最近 1-2 轮最重要，但必要时可以参考更早上下文。"},
		{Role: provider.RoleUser, Content: "主对话上下文如下：\n\n" + contextText},
	}
	start := 0
	if len(side) > 12 {
		start = len(side) - 12
	}
	for _, m := range side[start:] {
		role := provider.RoleUser
		if m.Role == "assistant" {
			role = provider.RoleAssistant
		}
		msgs = append(msgs, provider.Message{Role: role, Content: m.Content})
	}
	return msgs
}

func (a *App) sideChatMainContext(tabID string) string {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return "（主对话尚未就绪）"
	}
	history := historyMessages(ctrl.History(), sessionDisplayResolver(controllerSessionDir(ctrl), ctrl.SessionPath()))
	if len(history) == 0 {
		return "（主对话暂无可见消息）"
	}
	visible := make([]HistoryMessage, 0, len(history))
	for _, m := range history {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		visible = append(visible, m)
	}
	if len(visible) == 0 {
		return "（主对话暂无可见用户或 AI 消息）"
	}
	start := 0
	if len(visible) > 8 {
		start = len(visible) - 8
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString("更早上下文已省略；以下是最近主对话，最后 1-2 轮优先参考。\n\n")
	}
	for _, m := range visible[start:] {
		role := "用户"
		if m.Role == "assistant" {
			role = "AI"
		}
		content := strings.TrimSpace(m.Content)
		if len([]rune(content)) > 3000 {
			runes := []rune(content)
			content = string(runes[:3000]) + "\n（已截断）"
		}
		b.WriteString(role)
		b.WriteString("：\n")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func trimSideChatHistory(history []SideChatMessage) []SideChatMessage {
	const keep = 80
	if len(history) <= keep {
		return history
	}
	return append([]SideChatMessage(nil), history[len(history)-keep:]...)
}

func newSideChatID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
