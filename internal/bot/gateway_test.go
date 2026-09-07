package bot

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type gatewayRunnerFunc func(context.Context, string) error

func (f gatewayRunnerFunc) Run(ctx context.Context, input string) error { return f(ctx, input) }

// fakeAdapter 是一个内存中的假适配器，用于测试 BotGateway。
type fakeAdapter struct {
	mu       sync.Mutex
	platform Platform
	name     string
	msgCh    chan InboundMessage
	sent     []OutboundMessage
	started  bool
}

func newFakeAdapter(platform Platform, name string) *fakeAdapter {
	return &fakeAdapter{
		platform: platform,
		name:     name,
		msgCh:    make(chan InboundMessage, 16),
	}
}

func (f *fakeAdapter) Platform() Platform              { return f.platform }
func (f *fakeAdapter) Name() string                    { return f.name }
func (f *fakeAdapter) Messages() <-chan InboundMessage { return f.msgCh }

func (f *fakeAdapter) Start(ctx context.Context) error {
	f.mu.Lock()
	f.started = true
	f.mu.Unlock()
	return nil
}

func (f *fakeAdapter) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.msgCh != nil {
		close(f.msgCh)
		f.msgCh = nil
	}
	return nil
}

func (f *fakeAdapter) Send(ctx context.Context, msg OutboundMessage) (SendResult, error) {
	f.mu.Lock()
	f.sent = append(f.sent, msg)
	f.mu.Unlock()
	return SendResult{MessageID: "fake_msg_1"}, nil
}

func (f *fakeAdapter) SendTyping(ctx context.Context, chatID string) error { return nil }

func (f *fakeAdapter) sentMessages() []OutboundMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]OutboundMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

func TestFakeAdapterInterface(t *testing.T) {
	fa := newFakeAdapter(PlatformQQ, "fake-qq")

	if fa.Platform() != PlatformQQ {
		t.Error("wrong platform")
	}
	if fa.Name() != "fake-qq" {
		t.Error("wrong name")
	}

	ctx := context.Background()
	if err := fa.Start(ctx); err != nil {
		t.Fatal("start:", err)
	}
	if !fa.started {
		t.Error("should be started")
	}

	_, err := fa.Send(ctx, OutboundMessage{ChatID: "c1", Text: "hello"})
	if err != nil {
		t.Fatal("send:", err)
	}

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	if sent[0].Text != "hello" {
		t.Errorf("sent text = %q, want %q", sent[0].Text, "hello")
	}

	if err := fa.Stop(); err != nil {
		t.Fatal("stop:", err)
	}
}

func TestGatewayConstructAndStop(t *testing.T) {
	cfg := GatewayConfig{
		Model:         "test",
		MaxSteps:      10,
		WorkspaceRoot: ".",
		Enabled:       map[Platform]bool{PlatformQQ: true},
		Allowlist:     AllowlistConfig{Enabled: false},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, map[Platform]Adapter{
		PlatformQQ: newFakeAdapter(PlatformQQ, "fake-qq"),
	}, logger)

	// 网关不应该 panic
	if gw == nil {
		t.Fatal("gateway should not be nil")
	}
	gw.Stop()
}

func TestGatewayAllowlistCheck(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Users: map[Platform][]string{
				PlatformQQ: {"allowed_user_1"},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if !gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, UserID: "allowed_user_1"}) {
		t.Error("allowed user should pass")
	}
	if gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, UserID: "unknown_user"}) {
		t.Error("unknown user should not pass")
	}
	// 不同平台
	if gw.checkAllowlist(PlatformFeishu, InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, UserID: "allowed_user_1"}) {
		t.Error("QQ allowlist should not apply to feishu")
	}
}

func TestGatewayAllowlistDoesNotApplyGroupsToDirectMessages(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{
			Enabled: true,
			Users: map[Platform][]string{
				PlatformQQ: {"allowed_user"},
			},
			Groups: map[Platform][]string{
				PlatformQQ: {"allowed_group"},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if !gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDirect, ChatID: "guild-dm", UserID: "allowed_user"}) {
		t.Error("direct message should not be rejected by group allowlist")
	}
	if gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatGroup, ChatID: "unknown_group", UserID: "allowed_user"}) {
		t.Error("unknown group should still be rejected by group allowlist")
	}
}

func TestGatewayAllowlistDisabledRejectsByDefault(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{Enabled: false},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, UserID: "any_user"}) {
		t.Error("disabled allowlist should reject unless allow_all is explicit")
	}
}

func TestGatewayAllowAll(t *testing.T) {
	cfg := GatewayConfig{
		Allowlist: AllowlistConfig{AllowAll: true},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := NewGateway(cfg, nil, logger)

	if !gw.checkAllowlist(PlatformQQ, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, UserID: "any_user"}) {
		t.Error("allow_all should allow everyone")
	}
}

func TestGatewayStartListsRecentSessions(t *testing.T) {
	fa := newFakeAdapter(PlatformQQ, "fake-qq")
	cfg := GatewayConfig{
		Enabled:   map[Platform]bool{PlatformQQ: true},
		Allowlist: AllowlistConfig{AllowAll: true},
		SessionLister: func(limit int) ([]sessionChoice, error) {
			return []sessionChoice{
				{Number: 1, Title: "修复登录", Location: "proj-a", Turns: 3},
				{Number: 2, Title: "解释代码", Location: "独立工作区", Turns: 1},
			}, nil
		},
	}
	gw := NewGateway(cfg, map[Platform]Adapter{PlatformQQ: fa}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	gw.handleMessage(context.Background(), PlatformQQ, fa, InboundMessage{
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/start",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].Text, "1. 修复登录") || !strings.Contains(sent[0].Text, "2. 解释代码") {
		t.Fatalf("/start list text missing sessions: %q", sent[0].Text)
	}
}

func TestGatewayStartReportsEmptySessions(t *testing.T) {
	fa := newFakeAdapter(PlatformWeixin, "fake-weixin")
	cfg := GatewayConfig{
		Enabled:       map[Platform]bool{PlatformWeixin: true},
		Allowlist:     AllowlistConfig{AllowAll: true},
		SessionLister: func(limit int) ([]sessionChoice, error) { return nil, nil },
	}
	gw := NewGateway(cfg, map[Platform]Adapter{PlatformWeixin: fa}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	gw.handleMessage(context.Background(), PlatformWeixin, fa, InboundMessage{
		ChatType:  ChatDM,
		ChatID:    "chat-empty",
		UserID:    "user-1",
		Text:      "/start",
		MessageID: "msg-1",
	})

	sent := fa.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "暂无可恢复的对话") {
		t.Fatalf("empty list response = %#v", sent)
	}
}

func TestGatewaySharedStartRestoresOrcaWithoutListingEngineeringSessions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orca.jsonl")
	if err := agent.NewSession("sys").Save(path); err != nil {
		t.Fatal(err)
	}
	fa := newFakeAdapter(PlatformWeixin, "fake-weixin")
	listed := false
	restored := false
	gw := NewGateway(GatewayConfig{
		SharedAutomationSession: true,
		Allowlist:               AllowlistConfig{AllowAll: true},
		SessionLister: func(int) ([]sessionChoice, error) {
			listed = true
			return nil, nil
		},
		RestoreSession: func(context.Context, string, InboundMessage) (SessionChoice, bool, error) {
			restored = true
			return SessionChoice{TopicID: "orca", Path: path, Title: "Orca", WorkspaceRoot: dir}, true, nil
		},
		BuildSession: func(ctx context.Context, choice sessionChoice, sink event.Sink) (*control.Controller, error) {
			return control.New(control.Options{SessionDir: dir, SessionPath: choice.Path, Sink: sink}), nil
		},
	}, map[Platform]Adapter{PlatformWeixin: fa}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	gw.handleMessage(context.Background(), PlatformWeixin, fa, InboundMessage{
		ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "/start",
	})

	if listed {
		t.Fatal("shared /start must not list or select engineering conversations")
	}
	if !restored {
		t.Fatal("shared /start did not restore Orca")
	}
	if got, ok := gw.activeControllerKey(BuildSessionKey(SessionSource{Platform: PlatformWeixin, ChatType: ChatDM, ChatID: "chat", UserID: "user"})); !ok || got != controllerKeyForSession(path) {
		t.Fatalf("active shared session = %q, %v; want Orca", got, ok)
	}
	sent := fa.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "Orca 主对话") {
		t.Fatalf("shared /start response = %#v", sent)
	}
}

func TestGatewaySelectsListedSessionAndShowsAssistantTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "第一条问题"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "这是 AI 的一段比较长的回复，用来确认机器人进入历史会话之后，会把最后一段回复的结尾发送给用户。"})
	if err := sess.Save(path); err != nil {
		t.Fatalf("save session: %v", err)
	}

	fa := newFakeAdapter(PlatformFeishu, "fake-feishu")
	buildCalls := 0
	cfg := GatewayConfig{
		Enabled:   map[Platform]bool{PlatformFeishu: true},
		Allowlist: AllowlistConfig{AllowAll: true},
		SessionLister: func(limit int) ([]sessionChoice, error) {
			return []sessionChoice{{
				Number:        1,
				Path:          path,
				Title:         "历史调试",
				Location:      "proj-a",
				WorkspaceRoot: dir,
				Turns:         1,
				LastAssistant: "…最后一段回复的结尾发送给用户。",
			}}, nil
		},
		BuildSession: func(ctx context.Context, choice sessionChoice, sink event.Sink) (*control.Controller, error) {
			buildCalls++
			return control.New(control.Options{SessionDir: dir, SessionPath: path, Sink: sink, Label: "test"}), nil
		},
	}
	gw := NewGateway(cfg, map[Platform]Adapter{PlatformFeishu: fa}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	msg := InboundMessage{ChatType: ChatDM, ChatID: "chat-1", UserID: "user-1", MessageID: "msg-1"}
	msg.Text = "/start"
	gw.handleMessage(context.Background(), PlatformFeishu, fa, msg)
	msg.Text = "1"
	msg.MessageID = "msg-2"
	gw.handleMessage(context.Background(), PlatformFeishu, fa, msg)

	sent := fa.sentMessages()
	if len(sent) != 2 {
		t.Fatalf("sent count = %d, want 2: %#v", len(sent), sent)
	}
	if !strings.Contains(sent[1].Text, "已进入：历史调试") || !strings.Contains(sent[1].Text, "最后一段回复的结尾") {
		t.Fatalf("selection response = %q", sent[1].Text)
	}
	if buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1", buildCalls)
	}
	key := controllerKeyForSession(path)
	if _, ok := gw.controllers[key]; !ok {
		t.Fatalf("selected controller %q not registered", key)
	}
}

func TestGatewayNewCreatesAndSelectsSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new-session.jsonl")
	if err := agent.NewSession("sys").Save(path); err != nil {
		t.Fatalf("save new session: %v", err)
	}

	fa := newFakeAdapter(PlatformWeixin, "fake-weixin")
	var created bool
	var buildPath string
	cfg := GatewayConfig{
		Enabled:   map[Platform]bool{PlatformWeixin: true},
		Allowlist: AllowlistConfig{AllowAll: true},
		CreateSession: func(ctx context.Context, remoteKey string, msg InboundMessage) (SessionChoice, error) {
			created = true
			return SessionChoice{Path: path, Title: "机器人新对话", Location: "独立工作区", WorkspaceRoot: dir}, nil
		},
		BuildSession: func(ctx context.Context, choice sessionChoice, sink event.Sink) (*control.Controller, error) {
			buildPath = choice.Path
			return control.New(control.Options{SessionDir: dir, SessionPath: choice.Path, Sink: sink, Label: "test"}), nil
		},
	}
	gw := NewGateway(cfg, map[Platform]Adapter{PlatformWeixin: fa}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	gw.handleMessage(context.Background(), PlatformWeixin, fa, InboundMessage{
		ChatType:  ChatDM,
		ChatID:    "chat-1",
		UserID:    "user-1",
		Text:      "/new",
		MessageID: "msg-1",
	})

	if !created {
		t.Fatal("/new did not call CreateSession")
	}
	if buildPath != path {
		t.Fatalf("build path = %q, want %q", buildPath, path)
	}
	if _, ok := gw.controllers[controllerKeyForSession(path)]; !ok {
		t.Fatalf("new session controller not registered")
	}
	sent := fa.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "已创建新对话") {
		t.Fatalf("/new response = %#v", sent)
	}
}

func TestGatewayFirstMessageRestoresMappedAutomationSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restored.jsonl")
	if err := agent.NewSession("sys").Save(path); err != nil {
		t.Fatal(err)
	}
	created := false
	restored := false
	gw := NewGateway(GatewayConfig{
		Allowlist: AllowlistConfig{AllowAll: true},
		RestoreSession: func(ctx context.Context, remoteKey string, msg InboundMessage) (SessionChoice, bool, error) {
			restored = true
			return SessionChoice{TopicID: "auto-1", Path: path, Title: "已恢复", WorkspaceRoot: dir}, true, nil
		},
		CreateSession: func(context.Context, string, InboundMessage) (SessionChoice, error) {
			created = true
			return SessionChoice{}, nil
		},
		BuildSession: func(ctx context.Context, choice sessionChoice, sink event.Sink) (*control.Controller, error) {
			return control.New(control.Options{Runner: gatewayRunnerFunc(func(context.Context, string) error { return nil }), SessionPath: choice.Path, Sink: sink}), nil
		},
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	key, err := gw.ensureAutomationSession(context.Background(), "qq:chat-1", InboundMessage{Platform: PlatformQQ, Text: "继续"})
	if err != nil {
		t.Fatal(err)
	}
	if !restored || created || key != controllerKeyForSession(path) {
		t.Fatalf("restored=%v created=%v key=%q", restored, created, key)
	}
}

func TestGatewayContinuityDecisionAfterIdle(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	newPath := filepath.Join(dir, "new.jsonl")
	for _, path := range []string{oldPath, newPath} {
		if err := agent.NewSession("sys").Save(path); err != nil {
			t.Fatal(err)
		}
	}
	newGateway := func(related bool, decisionErr error) (*BotGateway, *int) {
		created := 0
		gw := NewGateway(GatewayConfig{
			Allowlist:         AllowlistConfig{AllowAll: true},
			ContinuityDecider: func(context.Context, SessionChoice, string) (bool, error) { return related, decisionErr },
			RestoreSession: func(context.Context, string, InboundMessage) (SessionChoice, bool, error) {
				t.Fatal("idle unrelated messages must create a fresh logical segment instead of restoring the old one")
				return SessionChoice{}, false, nil
			},
			CreateSession: func(context.Context, string, InboundMessage) (SessionChoice, error) {
				created++
				return SessionChoice{TopicID: "new", Path: newPath, Title: "新段", WorkspaceRoot: dir}, nil
			},
			BuildSession: func(ctx context.Context, choice sessionChoice, sink event.Sink) (*control.Controller, error) {
				return control.New(control.Options{Runner: gatewayRunnerFunc(func(context.Context, string) error { return nil }), SessionPath: choice.Path, Sink: sink}), nil
			},
		}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		gw.remoteStates["qq:chat"] = &remoteSessionState{mode: remoteModeInSession, selectedKey: controllerKeyForSession(oldPath), selectedPath: oldPath, title: "旧段", workspaceRoot: dir, lastActive: time.Now().Add(-31 * time.Minute)}
		return gw, &created
	}

	gw, created := newGateway(true, nil)
	key, err := gw.ensureAutomationSession(context.Background(), "qq:chat", InboundMessage{Text: "相关追问"})
	if err != nil || key != controllerKeyForSession(oldPath) || *created != 0 {
		t.Fatalf("related key=%q created=%d err=%v", key, *created, err)
	}
	gw, created = newGateway(false, nil)
	key, err = gw.ensureAutomationSession(context.Background(), "qq:chat", InboundMessage{Text: "新话题"})
	if err != nil || key != controllerKeyForSession(newPath) || *created != 1 {
		t.Fatalf("new key=%q created=%d err=%v", key, *created, err)
	}
}

func TestGatewaySharedAutomationSessionSwitchesEveryRemote(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.jsonl")
	secondPath := filepath.Join(dir, "second.jsonl")
	for _, path := range []string{firstPath, secondPath} {
		if err := agent.NewSession("sys").Save(path); err != nil {
			t.Fatal(err)
		}
	}
	gw := NewGateway(GatewayConfig{
		SharedAutomationSession: true,
		BuildSession: func(ctx context.Context, choice sessionChoice, sink event.Sink) (*control.Controller, error) {
			return control.New(control.Options{Runner: gatewayRunnerFunc(func(context.Context, string) error { return nil }), SessionPath: choice.Path, Sink: sink}), nil
		},
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := gw.selectSession(context.Background(), "weixin:one", SessionChoice{TopicID: "orca", Path: firstPath, Title: "Orca"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.selectSession(context.Background(), "qq:two", SessionChoice{TopicID: "orca", Path: secondPath, Title: "Orca"}); err != nil {
		t.Fatal(err)
	}
	want := controllerKeyForSession(secondPath)
	for _, remote := range []string{"weixin:one", "qq:two"} {
		if got, ok := gw.activeControllerKey(remote); !ok || got != want {
			t.Fatalf("remote %s selected %q, want shared %q", remote, got, want)
		}
	}
}

func TestGatewayGuidePersistsOnlyAfterSuccessfulTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guide.jsonl")
	if err := agent.NewSession("sys").Save(path); err != nil {
		t.Fatal(err)
	}
	adapter := newFakeAdapter(PlatformQQ, "qq")
	var ran atomic.Bool
	marked := 0
	gw := NewGateway(GatewayConfig{
		Allowlist: AllowlistConfig{AllowAll: true},
		CreateSession: func(context.Context, string, InboundMessage) (SessionChoice, error) {
			return SessionChoice{TopicID: "auto-guide", Path: path, Title: "指南", WorkspaceRoot: dir}, nil
		},
		BuildSession: func(ctx context.Context, choice sessionChoice, sink event.Sink) (*control.Controller, error) {
			return control.New(control.Options{Runner: gatewayRunnerFunc(func(context.Context, string) error { ran.Store(true); return nil }), SessionPath: choice.Path, Sink: sink}), nil
		},
		AfterGuideSent: func(Platform) error {
			if !ran.Load() {
				t.Fatal("guide persisted before the first turn completed")
			}
			marked++
			return nil
		},
	}, map[Platform]Adapter{PlatformQQ: adapter}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	msg := InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "你好"}
	gw.handleMessage(context.Background(), PlatformQQ, adapter, msg)
	gw.handleMessage(context.Background(), PlatformQQ, adapter, InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "再问一次"})
	if marked != 1 {
		t.Fatalf("guide persisted %d times, want once", marked)
	}
	if sent := adapter.sentMessages(); len(sent) != 1 || !strings.Contains(sent[0].Text, "使用指南") {
		t.Fatalf("guide messages = %#v", sent)
	}
}

func TestGatewayHiShowsAutomationCommands(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "weixin")
	gw := NewGateway(GatewayConfig{Allowlist: AllowlistConfig{AllowAll: true}}, map[Platform]Adapter{PlatformWeixin: adapter}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	gw.handleMessage(context.Background(), PlatformWeixin, adapter, InboundMessage{ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "/hi"})
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "/new") || !strings.Contains(sent[0].Text, "/continue") {
		t.Fatalf("/hi response = %#v", sent)
	}
}

func TestGatewayRejectedExternalAnswerKeepsPendingAsk(t *testing.T) {
	adapter := newFakeAdapter(PlatformQQ, "qq")
	const sourceID = "bot:owner"
	called := false
	gw := NewGateway(GatewayConfig{
		Allowlist: AllowlistConfig{AllowAll: true},
		ExternalAnswerForSource: func(gotSource, id string, answers []event.AskAnswer) ResponseRouteResult {
			called = true
			if gotSource != sourceID || id != "ask-1" {
				t.Fatalf("route received source=%q id=%q", gotSource, id)
			}
			return ResponseRejected
		},
	}, map[Platform]Adapter{PlatformQQ: adapter}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctrl := control.New(control.Options{SessionPath: "session-bot"})
	state := &sessionState{
		ctrl:        ctrl,
		pendingAsks: map[string][]event.AskQuestion{"ask-1": {{ID: "q1", Prompt: "continue?"}}},
		sourceID:    sourceID,
	}
	msg := InboundMessage{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "chat", UserID: "user", Text: "/answer ask-1 yes"}
	remoteKey := BuildSessionKey(msg.Session())
	gw.mu.Lock()
	gw.controllers["controller-1"] = state
	gw.remoteStates[remoteKey] = &remoteSessionState{mode: remoteModeInSession, selectedKey: "controller-1"}
	gw.mu.Unlock()

	gw.handleMessage(context.Background(), PlatformQQ, adapter, msg)
	if !called {
		t.Fatal("source-aware answer router was not called")
	}
	gw.mu.Lock()
	_, pending := state.pendingAsks["ask-1"]
	gw.mu.Unlock()
	if !pending {
		t.Fatal("rejected bot answer deleted pending ask")
	}
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "不属于当前来源") {
		t.Fatalf("rejection response = %#v", sent)
	}
}
