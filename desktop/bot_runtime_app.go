package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/bot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/bot/qq"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/bot/weixin"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/memory"
)

type BotRuntimeStatusView struct {
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	Channels []string `json:"channels"`
}

func (a *App) restartDesktopBotGateway() {
	cfg, err := config.Load()
	if err != nil {
		a.setBotRuntimeStatus("error", "读取机器人配置失败："+err.Error())
		return
	}
	a.startDesktopBotGateway(cfg)
}

func (a *App) restartDesktopBotGatewayWhenIdle() {
	a.mu.Lock()
	gw := a.botGateway
	if gw == nil || gw.ActiveCount() == 0 {
		a.mu.Unlock()
		a.restartDesktopBotGateway()
		return
	}
	if a.botGatewayRestartPending {
		a.mu.Unlock()
		return
	}
	a.botGatewayRestartPending = true
	a.mu.Unlock()

	go func(current *bot.BotGateway) {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if current.ActiveCount() != 0 {
					continue
				}
				a.mu.Lock()
				stillCurrent := a.botGateway == current
				a.botGatewayRestartPending = false
				a.mu.Unlock()
				if stillCurrent {
					a.restartDesktopBotGateway()
				}
				return
			case <-a.bootContext().Done():
				a.mu.Lock()
				a.botGatewayRestartPending = false
				a.mu.Unlock()
				return
			}
		}
	}(gw)
}

func (a *App) startDesktopBotGatewayOnStartup() {
	cfg, err := config.Load()
	if err != nil {
		a.setBotRuntimeStatus("error", "读取机器人配置失败："+err.Error())
		return
	}
	a.startDesktopBotGateway(cfg)
}

func (a *App) migrateDesktopBotPromptMode() {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil || cfg == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Bot.PromptMode), promptModeOrca) && strings.TrimSpace(cfg.Bot.WorkspaceRoot) == "" {
		return
	}
	cfg.Bot.PromptMode = promptModeOrca
	// The canonical Orca conversation is the only bot execution
	// root. Preserve legacy files on disk while ignoring custom root overrides.
	cfg.Bot.WorkspaceRoot = ""
	if err := cfg.SaveTo(path); err != nil {
		slog.Warn("could not migrate desktop bot prompt mode", "error", err)
	}
}

func (a *App) startDesktopBotGateway(cfg *config.Config) {
	a.stopDesktopBotGateway()
	if cfg == nil {
		a.setBotRuntimeStatus("error", "机器人配置为空。")
		return
	}

	cfg.Bot.Enabled = true
	cfg.Bot.Allowlist.Enabled = true
	cfg.Bot.Allowlist.AllowAll = true

	enabled := map[bot.Platform]bool{}
	adapters := map[bot.Platform]bot.Adapter{}
	channels := []string{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if qqConfigured(cfg.Bot.QQ) {
		enabled[bot.PlatformQQ] = true
		adapters[bot.PlatformQQ] = qq.New(cfg.Bot.QQ, logger)
		channels = append(channels, "QQ")
	}
	if weixinConfigured(cfg.Bot.Weixin) {
		enabled[bot.PlatformWeixin] = true
		adapters[bot.PlatformWeixin] = weixin.New(cfg.Bot.Weixin, logger)
		channels = append(channels, "微信")
	}
	if len(adapters) == 0 {
		a.setBotRuntimeStatus("idle", "QQ/微信尚未配置。")
		return
	}

	modelName := strings.TrimSpace(cfg.Bot.Model)
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	if resolved, _, ok := cfg.ResolveModelWithFallback(modelName); ok {
		modelName = resolved
	}
	workspaceRoot := automationWorkspaceRoot()

	ctx, cancel := context.WithCancel(context.Background())
	gw := bot.NewGateway(bot.GatewayConfig{
		Model:         modelName,
		PromptMode:    promptModeOrca,
		MemoryProfile: memory.ProfileAssistant,
		MaxSteps:      cfg.Bot.MaxSteps,
		WorkspaceRoot: workspaceRoot,
		Enabled:       enabled,
		Allowlist: bot.AllowlistConfig{
			Enabled:  true,
			AllowAll: true,
			Users:    map[bot.Platform][]string{},
			Groups:   map[bot.Platform][]string{},
		},
		Debounce:      time.Duration(cfg.Bot.DebounceMs) * time.Millisecond,
		CreateSession: a.createDesktopBotSession,
		BuildSession: func(ctx context.Context, choice bot.SessionChoice, sink event.Sink) (*control.Controller, error) {
			store, err := memory.EnsureCanonicalAssistantStore(config.MemoryUserDir())
			if err != nil {
				return nil, err
			}
			topicID := ""
			if meta, ok, _ := agent.LoadBranchMeta(choice.Path); ok {
				topicID = meta.TopicID
			}
			sessionWorkspaceRoot := strings.TrimSpace(choice.WorkspaceRoot)
			if sessionWorkspaceRoot == "" {
				sessionWorkspaceRoot = workspaceRoot
			}
			ctrl, err := a.buildController(ctx, boot.Options{
				Model: modelName, RuntimeProfile: promptModeOrca, MemoryProfile: memory.ProfileAssistant,
				AssistantMemoryStoreDir: store.Dir, MaxSteps: cfg.Bot.MaxSteps, RequireKey: true, Sink: sink,
				WorkspaceRoot: sessionWorkspaceRoot, SessionDir: filepath.Dir(choice.Path),
				ExtraTools: append(a.conversationBroker.Tools("bot:"+topicID, topicID), automationHistoryTool{
					topicID:     topicID,
					currentPath: func() string { return choice.Path },
				}),
				TurnContext: func() string { return a.conversationBroker.Index(topicID) },
				TurnLease:   a.sessionGate.Acquire, RefreshOnLease: true,
			})
			if err != nil {
				return nil, err
			}
			ctrl.EnableInteractiveApproval()
			ctrl.SetTrustedAutomationAccess(cfg.Desktop.AutomationFullAccess)
			return ctrl, nil
		},
		MirrorEvent:    a.mirrorBotEventToOpenTab,
		MirrorUser:     a.mirrorBotUserToOpenTab,
		AfterTurn:      func(sessionPath string) { a.afterDesktopBotTurn(sessionPath, modelName) },
		RestoreSession: a.restoreDesktopBotSession,
		GuideSent:      botGuideState(cfg),
		AfterGuideSent: a.markBotGuideSent,
		ContinuityDecider: func(ctx context.Context, previous bot.SessionChoice, currentMessage string) (bool, error) {
			return a.decideBotContinuity(ctx, modelName, previous, currentMessage)
		},
		RegisterExternalSink: a.conversationBroker.RegisterSourceSink,
		ExternalApproveForSource: func(sourceID, id string, allow bool) bot.ResponseRouteResult {
			return a.conversationBroker.ApproveForSource(sourceID, id, allow, false, false)
		},
		ExternalAnswerForSource: a.conversationBroker.AnswerForSource,
		ExternalStop:            a.conversationBroker.CancelActive,
		SharedAutomationSession: true,
	}, adapters, logger)
	if err := gw.Start(ctx); err != nil {
		cancel()
		a.setBotRuntimeStatus("error", "机器人后台网关启动失败："+err.Error())
		return
	}

	a.mu.Lock()
	a.botGateway = gw
	a.botGatewayCancel = cancel
	a.botRuntimeStatus = "running"
	a.botRuntimeErr = ""
	a.mu.Unlock()
	a.setBotRuntimeStatus("running", fmt.Sprintf("后台运行中：%s", strings.Join(channels, "、")))
}

func (a *App) stopDesktopBotGateway() {
	a.mu.Lock()
	gw := a.botGateway
	cancel := a.botGatewayCancel
	a.botGateway = nil
	a.botGatewayCancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if gw != nil {
		gw.Stop()
	}
}

func (a *App) setBotRuntimeStatus(status, message string) {
	a.mu.Lock()
	a.botRuntimeStatus = status
	a.botRuntimeErr = message
	a.mu.Unlock()
}

func (a *App) GetBotRuntimeStatus() (BotRuntimeStatusView, error) {
	cfg, _ := config.Load()
	channels := []string{}
	if cfg != nil {
		if qqConfigured(cfg.Bot.QQ) {
			channels = append(channels, "QQ")
		}
		if weixinConfigured(cfg.Bot.Weixin) {
			channels = append(channels, "微信")
		}
	}
	a.mu.RLock()
	status := a.botRuntimeStatus
	message := a.botRuntimeErr
	a.mu.RUnlock()
	if status == "" {
		status = "idle"
		message = "QQ/微信尚未配置。"
	}
	return BotRuntimeStatusView{Status: status, Message: message, Channels: channels}, nil
}

func qqConfigured(cfg config.QQBotConfig) bool {
	return strings.TrimSpace(cfg.AppID) != "" && strings.TrimSpace(cfg.AppSecretEnv) != "" && os.Getenv(cfg.AppSecretEnv) != ""
}

func weixinConfigured(cfg config.WeixinBotConfig) bool {
	if strings.TrimSpace(cfg.TokenEnv) != "" && os.Getenv(cfg.TokenEnv) != "" {
		return true
	}
	return weixin.HasSavedAccount(cfg.AccountID)
}
