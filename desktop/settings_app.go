package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/bot/weixin"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/memory"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/modelmeta"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/product"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/visioncap"
)

// settings_app.go is the desktop Settings panel's command surface: it reads the
// resolved config and applies edits through internal/config/edit.go (the
// purpose-built mutation API), then rebuilds the controller so the change takes
// effect live — the same snapshot→reload→resume pattern as SetModel. Secrets are
// the exception: they go to the global credentials file (upsertDotEnv), since
// config stores only the env-var name, not the key.

// --- read ---

type ProviderView struct {
	Name                string         `json:"name"`
	PresetID            string         `json:"presetId,omitempty"`
	Label               string         `json:"label,omitempty"`
	Description         string         `json:"description,omitempty"`
	Category            string         `json:"category,omitempty"`
	AccountURL          string         `json:"accountUrl,omitempty"`
	BuiltIn             bool           `json:"builtIn"`
	Added               bool           `json:"added"`
	Kind                string         `json:"kind"`
	BaseURL             string         `json:"baseUrl"`
	Models              []string       `json:"models"`
	ModelsURL           string         `json:"modelsUrl"`
	Default             string         `json:"default"`
	APIKeyEnv           string         `json:"apiKeyEnv"`
	KeySet              bool           `json:"keySet"` // the env var currently resolves to a non-empty value
	BalanceURL          string         `json:"balanceUrl"`
	ContextWindow       int            `json:"contextWindow"`
	ModelContextWindows map[string]int `json:"modelContextWindows,omitempty"`
	ReasoningProtocol   string         `json:"reasoningProtocol"`
	SupportedEfforts    []string       `json:"supportedEfforts"`
	DefaultEffort       string         `json:"defaultEffort"`
}

type PermissionsView struct {
	Mode            string   `json:"mode"`
	AutoReviewModel string   `json:"autoReviewModel"`
	Allow           []string `json:"allow"`
	Ask             []string `json:"ask"`
	Deny            []string `json:"deny"`
}

type SandboxView struct {
	Bash          string   `json:"bash"`
	Network       bool     `json:"network"`
	WorkspaceRoot string   `json:"workspaceRoot"`
	AllowWrite    []string `json:"allowWrite"`
}

type NetworkProxyView struct {
	Type     string `json:"type"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type NetworkView struct {
	ProxyMode string           `json:"proxyMode"`
	ProxyURL  string           `json:"proxyUrl"`
	NoProxy   string           `json:"noProxy"`
	Proxy     NetworkProxyView `json:"proxy"`
}

type AgentView struct {
	Temperature       float64 `json:"temperature"`
	MaxSteps          int     `json:"maxSteps"`
	PlannerMaxSteps   int     `json:"plannerMaxSteps"`
	SystemPrompt      string  `json:"systemPrompt"`
	SoftCompactRatio  float64 `json:"softCompactRatio"`
	CompactRatio      float64 `json:"compactRatio"`
	CompactForceRatio float64 `json:"compactForceRatio"`
}

type BotAllowlistView struct {
	Enabled      bool     `json:"enabled"`
	AllowAll     bool     `json:"allowAll"`
	QQUsers      []string `json:"qqUsers"`
	FeishuUsers  []string `json:"feishuUsers"`
	WeixinUsers  []string `json:"weixinUsers"`
	QQGroups     []string `json:"qqGroups"`
	FeishuGroups []string `json:"feishuGroups"`
	WeixinGroups []string `json:"weixinGroups"`
}

type QQBotView struct {
	Enabled      bool   `json:"enabled"`
	AppID        string `json:"appId"`
	AppSecretEnv string `json:"appSecretEnv"`
	SecretSet    bool   `json:"secretSet"`
	Environment  string `json:"environment"`
}

type FeishuBotView struct {
	Enabled           bool   `json:"enabled"`
	Domain            string `json:"domain"`
	AppID             string `json:"appId"`
	AppSecretEnv      string `json:"appSecretEnv"`
	SecretSet         bool   `json:"secretSet"`
	VerificationToken string `json:"verificationToken"`
	Mode              string `json:"mode"`
	WebhookPort       int    `json:"webhookPort"`
	RequireMention    bool   `json:"requireMention"`
}

type WeixinBotView struct {
	Enabled   bool   `json:"enabled"`
	AccountID string `json:"accountId"`
	TokenEnv  string `json:"tokenEnv"`
	TokenSet  bool   `json:"tokenSet"`
	APIBase   string `json:"apiBase"`
}

type BotSettingsView struct {
	Enabled       bool                `json:"enabled"`
	Model         string              `json:"model"`
	PromptMode    string              `json:"promptMode"`
	WorkspaceRoot string              `json:"workspaceRoot"`
	MaxSteps      int                 `json:"maxSteps"`
	DebounceMs    int                 `json:"debounceMs"`
	Allowlist     BotAllowlistView    `json:"allowlist"`
	QQ            QQBotView           `json:"qq"`
	Feishu        FeishuBotView       `json:"feishu"`
	Weixin        WeixinBotView       `json:"weixin"`
	Connections   []BotConnectionView `json:"connections"`
}

// SettingsView is the whole Settings panel payload.
type SettingsView struct {
	DefaultModel         string          `json:"defaultModel"`
	AutomationModel      string          `json:"automationModel"`
	PlannerModel         string          `json:"plannerModel"`
	SubagentModel        string          `json:"subagentModel"`
	VisionModel          string          `json:"visionModel"`
	EffectiveVisionModel string          `json:"effectiveVisionModel"`
	SubagentEffort       string          `json:"subagentEffort"`
	AutoPlan             string          `json:"autoPlan"`
	Providers            []ProviderView  `json:"providers"`
	OfficialProviders    []ProviderView  `json:"officialProviders"`
	Permissions          PermissionsView `json:"permissions"`
	Sandbox              SandboxView     `json:"sandbox"`
	Network              NetworkView     `json:"network"`
	Agent                AgentView       `json:"agent"`
	Bot                  BotSettingsView `json:"bot"`
	DesktopLanguage      string          `json:"desktopLanguage"`
	DesktopTheme         string          `json:"desktopTheme"`
	DesktopThemeStyle    string          `json:"desktopThemeStyle"`
	DesktopUIStyle       string          `json:"desktopUIStyle"`
	CloseBehavior        string          `json:"closeBehavior"`
	CheckUpdates         bool            `json:"checkUpdates"`
	ExpandThinking       bool            `json:"expandThinking"`
	ProcessDisplayMode   string          `json:"processDisplayMode"`
	ActivityIndicator    bool            `json:"activityIndicatorEnabled"`
	VisionEnabled        bool            `json:"visionEnabled"`
	VisionMode           string          `json:"visionMode"`
	UIScale              int             `json:"uiScale"`
	EffectiveUIScale     int             `json:"effectiveUIScale"`
	AutomationFullAccess bool            `json:"automationFullAccessApproved"`
	ComputerControlModel string          `json:"computerControlModel"`
	ComputerUseApproved  bool            `json:"computerUseFullAccessApproved"`
	ConfigPath           string          `json:"configPath"`
	// ProviderKinds lists the provider implementations the kernel actually
	// registered (provider.Kinds()), so the editor's "kind" picker offers only
	// kinds that resolve — selecting an unregistered one would fail the rebuild.
	ProviderKinds []string `json:"providerKinds"`
	// AutoApproveTools is the live YOLO/full-access state (runtime-only, not from
	// config), so the panel's toggle reflects whether tool approvals are currently
	// being skipped this session.
	AutoApproveTools bool `json:"autoApproveTools"`
	// Bypass is the legacy JSON key for the same live state.
	Bypass bool `json:"bypass"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func cloneStringIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func providerRemovalFallbackRef(c *config.Config, name string) string {
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == name || !p.Configured() || len(p.ModelList()) == 0 {
			continue
		}
		return p.Name + "/" + p.DefaultModel()
	}
	return ""
}

func desktopModelRefsProvider(c *config.Config, ref, name string) bool {
	if config.ModelRefsProvider(ref, name) {
		return true
	}
	if e, ok := c.ResolveModel(ref); ok {
		return e.Name == name
	}
	return false
}

func officialProviderHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func officialProviderKindFromEntry(p config.ProviderEntry) string {
	host := officialProviderHost(p.BaseURL)
	if preset, ok := config.ProviderPresetByID(p.Name); ok && host == officialProviderHost(preset.Entry.BaseURL) {
		return preset.ID
	}
	switch config.CanonicalDesktopOfficialProviderName(p.Name) {
	case "deepseek":
		if host == "api.deepseek.com" {
			return "deepseek"
		}
	case "mimo-api":
		if host == "api.xiaomimimo.com" {
			return "mimo-api"
		}
	case "mimo-token-plan":
		if host == "token-plan-cn.xiaomimimo.com" {
			return "mimo-token-plan"
		}
	}
	return ""
}

func isOfficialBuiltInProvider(p config.ProviderEntry) bool {
	return officialProviderKindFromEntry(p) != ""
}

func providerAccessSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func addProviderAccess(c *config.Config, names ...string) {
	seen := providerAccessSet(c.Desktop.ProviderAccess)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		c.Desktop.ProviderAccess = append(c.Desktop.ProviderAccess, name)
		seen[name] = true
	}
}

func removeProviderAccess(c *config.Config, names ...string) {
	remove := providerAccessSet(names)
	if len(remove) == 0 {
		return
	}
	out := c.Desktop.ProviderAccess[:0]
	for _, name := range c.Desktop.ProviderAccess {
		if !remove[name] {
			out = append(out, name)
		}
	}
	c.Desktop.ProviderAccess = out
}

func providerViewFromEntry(p config.ProviderEntry, builtIn, added bool) ProviderView {
	v := ProviderView{
		Name: p.Name, BuiltIn: builtIn, Added: added, Kind: p.Kind, BaseURL: p.BaseURL,
		Models: nonNil(p.ChatModelList()), ModelsURL: p.ModelsURL, Default: p.DefaultModel(),
		APIKeyEnv:           p.APIKeyEnv,
		KeySet:              p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) != "",
		BalanceURL:          p.BalanceURL,
		ContextWindow:       p.ContextWindow,
		ModelContextWindows: cloneStringIntMap(p.ModelContextWindows),
		ReasoningProtocol:   p.ReasoningProtocol,
		SupportedEfforts:    nonNil(p.SupportedEfforts),
		DefaultEffort:       p.DefaultEffort,
	}
	if preset, ok := config.ProviderPresetByID(officialProviderKindFromEntry(p)); ok {
		v.PresetID = preset.ID
		v.Label = preset.Label
		v.Description = preset.Description
		v.Category = preset.Category
		v.AccountURL = preset.AccountURL
	}
	return v
}

func officialProviderViews(added map[string]bool) []ProviderView {
	var out []ProviderView
	for _, preset := range config.ProviderPresetCatalog() {
		v := providerViewFromEntry(preset.Entry, true, added[preset.ID] || added[preset.Entry.Name])
		v.PresetID, v.Label, v.Description, v.Category, v.AccountURL = preset.ID, preset.Label, preset.Description, preset.Category, preset.AccountURL
		out = append(out, v)
	}
	return out
}

func officialProviderAddedSet(cfg *config.Config) map[string]bool {
	out := map[string]bool{}
	if cfg == nil {
		return out
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	for i := range cfg.Providers {
		p := cfg.Providers[i]
		if !access[p.Name] {
			continue
		}
		if kind := officialProviderKindFromEntry(p); kind != "" {
			out[kind] = true
		}
	}
	return out
}

// Settings returns the current configuration for the Settings panel.
func (a *App) Settings() SettingsView {
	cfg, cfgPath, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return SettingsView{
			Providers:         []ProviderView{},
			OfficialProviders: officialProviderViews(map[string]bool{}),
			ProviderKinds:     nonNil(provider.Kinds()),
			Permissions: PermissionsView{
				Mode:            "ask",
				AutoReviewModel: "",
				Allow:           []string{},
				Ask:             []string{},
				Deny:            []string{},
			},
			Sandbox:              SandboxView{Bash: "enforce", AllowWrite: []string{}},
			Agent:                AgentView{PlannerMaxSteps: 12, SoftCompactRatio: 0.5, CompactRatio: 0.8, CompactForceRatio: 0.9},
			Bot:                  botSettingsView(config.BotConfig{}),
			AutoPlan:             "off",
			DesktopTheme:         "light",
			DesktopThemeStyle:    "slate",
			DesktopUIStyle:       config.DesktopUIStyleModern,
			CloseBehavior:        "background",
			CheckUpdates:         true,
			ExpandThinking:       false,
			ProcessDisplayMode:   config.ProcessDisplayCompact,
			ActivityIndicator:    true,
			VisionEnabled:        false,
			VisionMode:           config.VisionModeAuto,
			UIScale:              0,
			EffectiveUIScale:     100,
			AutomationFullAccess: false,
		}
	}
	ctrl := a.activeCtrl()
	bash := cfg.Sandbox.Bash
	if bash == "" {
		bash = "enforce"
	}
	v := SettingsView{
		DefaultModel:         cfg.DefaultModel,
		AutomationModel:      cfg.Bot.Model,
		PlannerModel:         cfg.Agent.PlannerModel,
		SubagentModel:        cfg.Agent.SubagentModel,
		VisionModel:          cfg.Agent.SubagentModels[config.VisionSubagentRole],
		EffectiveVisionModel: cfg.ResolveVisionModelRef(),
		SubagentEffort:       cfg.Agent.SubagentEffort,
		AutoPlan:             desktopAutoPlanMode(cfg.Agent.AutoPlan),
		Providers:            []ProviderView{},
		OfficialProviders:    []ProviderView{},
		Permissions: PermissionsView{
			Mode:            orDefault(cfg.Permissions.Mode, "ask"),
			AutoReviewModel: cfg.Permissions.AutoReviewModel,
			Allow:           nonNil(cfg.Permissions.Allow),
			Ask:             nonNil(cfg.Permissions.Ask),
			Deny:            nonNil(cfg.Permissions.Deny),
		},
		Sandbox: SandboxView{
			Bash: bash, Network: cfg.Sandbox.Network,
			WorkspaceRoot: cfg.Sandbox.WorkspaceRoot, AllowWrite: nonNil(cfg.Sandbox.AllowWrite),
		},
		Network: NetworkView{
			ProxyMode: cfg.NetworkProxyMode(),
			ProxyURL:  cfg.Network.ProxyURL,
			NoProxy:   cfg.Network.NoProxy,
			Proxy: NetworkProxyView{
				Type:     orDefault(cfg.Network.Proxy.Type, "socks5"),
				Server:   cfg.Network.Proxy.Server,
				Port:     cfg.Network.Proxy.Port,
				Username: cfg.Network.Proxy.Username,
				Password: cfg.Network.Proxy.Password,
			},
		},
		Agent:                AgentView{Temperature: cfg.Agent.Temperature, MaxSteps: cfg.Agent.MaxSteps, PlannerMaxSteps: cfg.Agent.PlannerMaxSteps, SystemPrompt: cfg.Agent.SystemPrompt, SoftCompactRatio: compactRatioOrDefault(cfg.Agent.SoftCompactRatio, 0.5), CompactRatio: compactRatioOrDefault(cfg.Agent.CompactRatio, 0.8), CompactForceRatio: compactRatioOrDefault(cfg.Agent.CompactForceRatio, 0.9)},
		Bot:                  botSettingsView(cfg.Bot),
		DesktopLanguage:      cfg.DesktopLanguage(),
		DesktopTheme:         "light",
		DesktopThemeStyle:    "slate",
		DesktopUIStyle:       cfg.DesktopUIStyle(),
		CloseBehavior:        cfg.DesktopCloseBehavior(),
		CheckUpdates:         cfg.DesktopCheckUpdates(),
		ExpandThinking:       cfg.DesktopProcessDisplayMode() == config.ProcessDisplayDetailed,
		ProcessDisplayMode:   cfg.DesktopProcessDisplayMode(),
		ActivityIndicator:    cfg.Desktop.ActivityIndicator,
		VisionEnabled:        cfg.Desktop.VisionEnabled,
		VisionMode:           cfg.DesktopVisionMode(),
		UIScale:              0,
		EffectiveUIScale:     100,
		AutomationFullAccess: cfg.Desktop.AutomationFullAccess,
		ComputerControlModel: strings.TrimSpace(cfg.Desktop.ComputerControlModel),
		ComputerUseApproved:  cfg.Desktop.ComputerUseFullAccess && cfg.Desktop.ComputerUseConsent == computerUseConsentVersion,
		ConfigPath:           cfgPath,
		ProviderKinds:        nonNil(provider.Kinds()),
		AutoApproveTools:     ctrl != nil && ctrl.AutoApproveTools(),
		Bypass:               ctrl != nil && ctrl.AutoApproveTools(),
	}
	if resolved, _, ok := cfg.ResolveModelWithFallback(v.AutomationModel); ok {
		v.AutomationModel = resolved
	} else {
		v.AutomationModel = cfg.DefaultModel
	}
	added := providerAccessSet(cfg.Desktop.ProviderAccess)
	v.OfficialProviders = officialProviderViews(officialProviderAddedSet(cfg))
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		v.Providers = append(v.Providers, providerViewFromEntry(*p, isOfficialBuiltInProvider(*p), added[p.Name]))
	}
	return v
}

func botSettingsView(b config.BotConfig) BotSettingsView {
	mode := strings.TrimSpace(b.Feishu.Mode)
	if mode == "" {
		mode = "webhook"
	}
	return BotSettingsView{
		Enabled:       true,
		Model:         b.Model,
		PromptMode:    promptModeOrca,
		WorkspaceRoot: automationWorkspaceRoot(),
		MaxSteps:      b.MaxSteps,
		DebounceMs:    b.DebounceMs,
		Allowlist: BotAllowlistView{
			Enabled:      true,
			AllowAll:     true,
			QQUsers:      nonNil(b.Allowlist.QQUsers),
			FeishuUsers:  nonNil(b.Allowlist.FeishuUsers),
			WeixinUsers:  nonNil(b.Allowlist.WeixinUsers),
			QQGroups:     nonNil(b.Allowlist.QQGroups),
			FeishuGroups: nonNil(b.Allowlist.FeishuGroups),
			WeixinGroups: nonNil(b.Allowlist.WeixinGroups),
		},
		QQ: QQBotView{
			Enabled:      b.QQ.Enabled,
			AppID:        b.QQ.AppID,
			AppSecretEnv: b.QQ.AppSecretEnv,
			SecretSet:    strings.TrimSpace(b.QQ.AppSecretEnv) != "" && os.Getenv(b.QQ.AppSecretEnv) != "",
			Environment:  qqEnvironmentOrDefault(b.QQ.Environment),
		},
		Feishu: FeishuBotView{
			Enabled:           false,
			Domain:            orDefault(strings.TrimSpace(b.Feishu.Domain), "feishu"),
			AppID:             b.Feishu.AppID,
			AppSecretEnv:      b.Feishu.AppSecretEnv,
			SecretSet:         strings.TrimSpace(b.Feishu.AppSecretEnv) != "" && os.Getenv(b.Feishu.AppSecretEnv) != "",
			VerificationToken: b.Feishu.VerificationToken,
			Mode:              mode,
			WebhookPort:       b.Feishu.WebhookPort,
			RequireMention:    b.Feishu.RequireMention,
		},
		Weixin: WeixinBotView{
			Enabled:   b.Weixin.Enabled,
			AccountID: b.Weixin.AccountID,
			TokenEnv:  b.Weixin.TokenEnv,
			TokenSet:  weixinTokenAvailable(b.Weixin),
			APIBase:   b.Weixin.APIBase,
		},
		Connections: botConnectionViews(b.Connections),
	}
}

func weixinTokenAvailable(c config.WeixinBotConfig) bool {
	if strings.TrimSpace(c.TokenEnv) != "" && os.Getenv(c.TokenEnv) != "" {
		return true
	}
	return weixin.HasSavedAccount(c.AccountID)
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func qqEnvironmentOrDefault(env string) string {
	if strings.EqualFold(strings.TrimSpace(env), "sandbox") {
		return "sandbox"
	}
	return "production"
}

func botDomainOrDefault(domain string) string {
	if strings.EqualFold(strings.TrimSpace(domain), "lark") {
		return "lark"
	}
	return "feishu"
}

// --- apply (write config, then rebuild the controller so it's live) ---

// applyConfigChange mutates the user-global config and rebuilds the controller so
// the change takes effect this session. Desktop settings such as providers and
// keys are account-level, not per-project: writing them to the global config
// rather than the cwd's orca.toml is what lets them survive a workspace switch.
func (a *App) applyConfigChange(mutate func(*config.Config) error) error {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	return a.rebuild()
}

func (a *App) applyConfigOnly(mutate func(*config.Config) error) error {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

func (a *App) loadDesktopUserConfigForEdit() (*config.Config, string, error) {
	userPath := config.UserConfigPath()
	if userPath == "" {
		return nil, "", fmt.Errorf("cannot resolve user config directory")
	}
	if _, err := os.Stat(userPath); err == nil {
		cfg := config.LoadForEdit(userPath)
		normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath)
		return cfg, userPath, nil
	}
	cfg := config.LoadForEdit(userPath)
	legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath)
		return cfg, userPath, nil
	}
	legacyCfg := config.LoadForEdit(legacyPath)
	normalizeLegacyDesktopProviderAccessForSettings(legacyCfg, legacyPath)
	legacyCfg.ConfigVersion = config.Default().ConfigVersion
	return legacyCfg, userPath, nil
}

func normalizeLegacyDesktopProviderAccessForSettings(cfg *config.Config, path string) {
	if cfg == nil || len(cfg.Desktop.ProviderAccess) > 0 || configDeclaresProviderAccess(path) {
		return
	}
	config.NormalizeLegacyDesktopProviderAccess(cfg)
}

func configDeclaresProviderAccess(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		if before, _, ok := strings.Cut(line, "#"); ok {
			line = before
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "provider_access") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "provider_access"))
			return strings.HasPrefix(rest, "=")
		}
	}
	return false
}

func (a *App) activeWorkspaceRoot() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab := a.activeTabLocked(); tab != nil {
		return tab.WorkspaceRoot
	}
	return "."
}

func projectConfigPathForRoot(root string) string {
	var canonical string
	if strings.TrimSpace(root) == "" || root == "." {
		canonical = product.ProjectConfigName
	} else {
		canonical = filepath.Join(root, product.ProjectConfigName)
	}
	if _, err := os.Stat(canonical); err == nil {
		return canonical
	}
	legacy := product.LegacyProjectConfigName
	if strings.TrimSpace(root) != "" && root != "." {
		legacy = filepath.Join(root, legacy)
	}
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return canonical
}

func sameConfigPath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr == nil && bErr == nil {
		return filepath.Clean(aAbs) == filepath.Clean(bAbs)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// rebuild tears down the controller and rebuilds it from the (just-changed)
// config, carrying the conversation forward. It keeps the active model if it
// still resolves; otherwise it falls back to the new default. Mirrors SetModel.
func (a *App) rebuild() error {
	if a.ctx == nil {
		return nil
	}
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active tab")
	}
	generation := a.beginTabRuntimeReconfigure(tab)
	tab.runtimeMu.Lock()
	defer tab.runtimeMu.Unlock()
	if !a.tabRuntimeGenerationCurrent(tab, generation) {
		return nil
	}
	success := false
	defer func() { a.finishTabRuntimeReconfigure(tab, generation, success) }()
	var carried []provider.Message
	prevPath := ""
	oldCtrl := tab.Ctrl
	if oldCtrl != nil {
		prevPath = oldCtrl.SessionPath()
		_ = oldCtrl.Snapshot()
		carried = oldCtrl.History()
	}
	model := tab.model
	if cfg, err := config.LoadForRoot(tab.WorkspaceRoot); err == nil {
		if resolved, fallback, ok := cfg.ResolveModelWithFallback(model); ok {
			if fallback && strings.TrimSpace(model) != "" {
				a.noticeForTab(tab.ID, fmt.Sprintf("model %q is no longer available; switched to %s", model, resolved))
			}
			model = resolved
		}
	}
	ctrl, err := a.buildController(a.bootContext(), boot.Options{
		Model: model, RequireKey: false,
		Sink:                    tab.sink,
		WorkspaceRoot:           tab.WorkspaceRoot,
		SessionDir:              tabSessionDir(tab),
		EffortOverride:          cloneStringPtr(tab.effort),
		RuntimeProfile:          currentTabPromptMode(tab),
		MemoryProfile:           conversationMemoryProfile(currentTabPromptMode(tab)),
		AssistantMemoryStoreDir: assistantStoreDirForMode(currentTabPromptMode(tab)),
	})
	if err != nil {
		a.mu.Lock()
		tab.StartupErr = err.Error()
		tab.Ready = true
		a.mu.Unlock()
		a.emitReady(a.ctx)
		return err
	}
	a.bindControllerDisplayRecorder(ctrl)
	ctrl.EnableInteractiveApproval()
	applyTabModeToController(ctrl, tab.mode)
	applyTabToolApprovalModeToController(ctrl, tab.toolApprovalMode)
	ctrl.SetAskWorkflow(tab.askWorkflow)
	ctrl.SetStepThinking(tab.stepThinking)
	ctrl.SetGoal(tab.goal)
	path := agent.ContinueSessionPath(prevPath, ctrl.SessionDir(), ctrl.Label())
	if len(carried) > 0 {
		carried = withFreshSystemPrompt(carried, systemPromptFrom(ctrl.History()))
		ctrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		ctrl.SetSessionPath(path)
	}
	a.mu.Lock()
	if current := a.tabs[tab.ID]; current != tab || tab.runtimeGeneration != generation {
		closed := current != tab
		a.mu.Unlock()
		ctrl.Close()
		if closed {
			return fmt.Errorf("conversation closed while rebuilding settings")
		}
		return nil
	}
	tab.Ctrl = ctrl
	tab.model = model
	tab.Label = ctrl.Label()
	tab.StartupErr = ""
	tab.Ready = true
	a.saveTabsLocked()
	a.mu.Unlock()
	if oldCtrl != nil {
		oldCtrl.Close()
	}
	a.emitReady(a.ctx)
	a.persistTabSessionPath(tab, path)
	success = true
	return nil
}

func systemPromptFrom(messages []provider.Message) string {
	for _, m := range messages {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

func withFreshSystemPrompt(messages []provider.Message, system string) []provider.Message {
	if strings.TrimSpace(system) == "" {
		return messages
	}
	out := append([]provider.Message(nil), messages...)
	for i := range out {
		if out[i].Role == provider.RoleSystem {
			out[i].Content = system
			out[i].ReasoningContent = ""
			out[i].ReasoningSignature = ""
			out[i].ToolCalls = nil
			out[i].ToolCallID = ""
			out[i].Name = ""
			return out
		}
	}
	return append([]provider.Message{{Role: provider.RoleSystem, Content: system}}, out...)
}

// SetDefaultModel sets the config default and switches the live model to it.
func (a *App) SetDefaultModel(ref string) error {
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active tab")
	}
	prev := tab.model
	tab.model = ref
	if err := a.applyConfigChange(func(c *config.Config) error {
		resolved, err := selectableDesktopModelRef(c, ref)
		if err != nil {
			return err
		}
		c.DefaultModel = resolved
		tab.model = resolved
		return nil
	}); err != nil {
		tab.model = prev
		return err
	}
	return nil
}

// SetPlannerModel sets (or, with "", clears) the two-model planner.
func (a *App) SetPlannerModel(ref string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		if ref != "" {
			resolved, err := selectableDesktopModelRef(c, ref)
			if err != nil {
				return err
			}
			ref = resolved
		}
		c.Agent.PlannerModel = ref
		return nil
	})
}

// SetSubagentModel sets (or clears) the default model used by subagent entry points.
func (a *App) SetSubagentModel(ref string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			resolved, err := selectableDesktopModelRef(c, ref)
			if err != nil {
				return err
			}
			ref = resolved
		}
		c.Agent.SubagentModel = ref
		return nil
	})
}

// SetVisionModel changes only the explicit image-task role, not general subagents.
func (a *App) SetVisionModel(ref string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			resolved, err := selectableDesktopModelRef(c, ref)
			if err != nil {
				return err
			}
			ref = resolved
		}
		return c.SetVisionModel(ref)
	})
}

func selectableDesktopModelRef(c *config.Config, ref string) (string, error) {
	entry, ok := c.ResolveModel(ref)
	if !ok {
		return "", fmt.Errorf("unknown model %q", ref)
	}
	if !modelProviderAccessAllowed(providerAccessSet(c.Desktop.ProviderAccess), entry.Name) {
		return "", fmt.Errorf("model %q is not available because provider %q is not added", ref, entry.Name)
	}
	if !entry.Configured() {
		return "", fmt.Errorf("model %q is not available because provider %q has no key", ref, entry.Name)
	}
	return entry.Name + "/" + entry.Model, nil
}

// SetSubagentEffort sets (or clears) the default effort used by subagent entry points.
func (a *App) SetSubagentEffort(level string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		level = strings.TrimSpace(level)
		if level == "" || level == "auto" {
			c.Agent.SubagentEffort = ""
			return nil
		}
		model := strings.TrimSpace(c.Agent.SubagentModel)
		if model == "" {
			model = c.DefaultModel
		}
		entry, ok := c.ResolveModel(model)
		if !ok {
			return fmt.Errorf("unknown subagent model %q", model)
		}
		effort, err := config.NormalizeEffort(entry, level)
		if err != nil {
			return err
		}
		c.Agent.SubagentEffort = effort
		return nil
	})
}

// SetAutoPlan updates the automatic plan-mode gate (off|on).
func (a *App) SetAutoPlan(mode string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.SetAutoPlan(mode) })
}

func desktopAutoPlanMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on", "ask":
		return "on"
	default:
		return "off"
	}
}

func officialProviderTemplate(kind string) ([]config.ProviderEntry, string, error) {
	id := strings.ToLower(strings.TrimSpace(kind))
	switch id {
	case "deepseek-official":
		id = "deepseek"
	case "xiaomi-mimo", "xiaomi_mimo":
		id = "mimo-api"
	case "xiaomi-mimo-token-plan", "xiaomi_mimo_token_plan":
		id = "mimo-token-plan"
	}
	preset, ok := config.ProviderPresetByID(id)
	if !ok {
		return nil, "", fmt.Errorf("unknown official provider template %q", kind)
	}
	return []config.ProviderEntry{preset.Entry}, preset.Entry.APIKeyEnv, nil
}

func chatProviderModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] || !config.IsLikelyChatModel(model) {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func providerDefaultForModels(currentDefault string, models []string) string {
	currentDefault = strings.TrimSpace(currentDefault)
	if currentDefault != "" {
		for _, model := range models {
			if model == currentDefault {
				return currentDefault
			}
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

// SaveProvider adds or updates a provider. A single model fills `model`; several
// fill `models` (with `default`). The shared key/endpoint live on the entry.
func (a *App) SaveProvider(p ProviderView) error {
	err := a.applyConfigChange(func(c *config.Config) error {
		e := config.ProviderEntry{Name: p.Name}
		for i := range c.Providers {
			if c.Providers[i].Name == p.Name {
				e = c.Providers[i]
				break
			}
		}
		e.Name = p.Name
		e.Kind = p.Kind
		e.BaseURL = p.BaseURL
		e.ModelsURL = p.ModelsURL
		e.APIKeyEnv = p.APIKeyEnv
		e.BalanceURL = strings.TrimSpace(p.BalanceURL)
		e.ContextWindow = p.ContextWindow
		if p.ModelContextWindows != nil {
			e.ModelContextWindows = cloneStringIntMap(p.ModelContextWindows)
		}
		e.ReasoningProtocol = p.ReasoningProtocol
		e.SupportedEfforts = p.SupportedEfforts
		e.DefaultEffort = p.DefaultEffort
		e.Model = ""
		e.Models = nil
		e.Default = ""
		models := chatProviderModels(p.Models)
		if len(models) > 0 {
			e.Model = models[0] // also satisfies validateProvider's model requirement
			if len(models) > 1 {
				e.Models = models
				e.Default = providerDefaultForModels(p.Default, models)
			}
		}
		if err := c.UpsertProvider(e); err != nil {
			return err
		}
		addProviderAccess(c, p.Name)
		return nil
	})
	if err == nil {
		refs := make([]string, 0, len(p.Models))
		for _, model := range p.Models {
			refs = append(refs, p.Name+"/"+model)
		}
		a.scheduleVisionProbes(refs)
	}
	return err
}

// UpdateProviderModels updates only the model selection for an existing
// provider. Background discovery must not write a stale ProviderView over
// fields that the user may have changed while the request was in flight.
func (a *App) UpdateProviderModels(name string, models []string, defaultModel string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("update provider models: empty provider name")
	}
	err := a.applyConfigChange(func(c *config.Config) error {
		p, ok := c.Provider(name)
		if !ok {
			return fmt.Errorf("update provider models: provider %q not found", name)
		}
		models = chatProviderModels(models)
		if len(models) == 0 {
			return fmt.Errorf("update provider models: provider %q returned no chat models", name)
		}
		p.Model = models[0]
		p.Models = nil
		p.Default = ""
		if len(models) > 1 {
			p.Models = append([]string(nil), models...)
			p.Default = providerDefaultForModels(defaultModel, models)
			if p.Default == "" {
				p.Default = providerDefaultForModels(p.DefaultModel(), models)
			}
		}
		addProviderAccess(c, name)
		return nil
	})
	if err == nil {
		refs := make([]string, 0, len(models))
		for _, model := range models {
			refs = append(refs, name+"/"+model)
		}
		a.scheduleVisionProbes(refs)
	}
	return err
}

// AddOfficialProviderAccess adds one curated desktop provider template to the
// Settings > Model > Access list. The runtime default providers still exist
// independently; this only records the user's explicit access setup.
func (a *App) AddOfficialProviderAccess(kind, key string) error {
	entries, keyEnv, err := officialProviderTemplate(kind)
	if err != nil {
		return err
	}
	if strings.TrimSpace(key) != "" && keyEnv != "" {
		if err := upsertDotEnv(keyEnv, key); err != nil {
			return err
		}
	}
	err = a.applyConfigChange(func(c *config.Config) error {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if err := c.UpsertProvider(e); err != nil {
				return err
			}
			names = append(names, e.Name)
		}
		addProviderAccess(c, names...)
		return nil
	})
	if err != nil {
		return err
	}
	refs := make([]string, 0)
	for _, entry := range entries {
		for _, model := range entry.ChatModelList() {
			refs = append(refs, entry.Name+"/"+model)
		}
	}
	a.scheduleVisionProbes(refs)
	return nil
}

// FetchProviderModels probes the provider's OpenAI-compatible model-list
// endpoint and returns the available model IDs. This is a settings-only helper:
// it never touches chat request serialization or provider-visible prompt data.
func (a *App) FetchProviderModels(p ProviderView) ([]string, error) {
	e := config.ProviderEntry{
		Name:      p.Name,
		Kind:      p.Kind,
		BaseURL:   p.BaseURL,
		ModelsURL: p.ModelsURL,
		APIKeyEnv: p.APIKeyEnv,
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), 15*time.Second)
	defer cancel()
	metadata, err := e.FetchModelMetadata(ctx)
	if err != nil {
		return []string{}, err
	}
	models := make([]string, 0, len(metadata))
	store := visioncap.Load("")
	metadataStore := modelmeta.Load("")
	for _, item := range metadata {
		models = append(models, item.ID)
		entry := e
		entry.Model = item.ID
		_ = metadataStore.Put(modelmeta.MetadataFromDiscovery(
			&entry, item.ContextWindow, item.ContextReason, modelmeta.Status(item.Vision),
			modelmeta.Status(item.ToolUse), modelmeta.Status(item.StructuredOutput), item.Pricing,
		))
		if item.Vision == nil {
			continue
		}
		current := store.Stored(&entry)
		status := visioncap.Unsupported
		if *item.Vision {
			status = visioncap.Supported
		}
		current.ModelRef = visioncap.ModelRef(&entry)
		current.Key = visioncap.Key(&entry)
		current.Status = status
		current.Source = visioncap.SourceMetadata
		current.Reason = "provider model metadata: " + item.VisionReason
		current.CheckedAt = time.Now().UnixMilli()
		_ = store.Put(current)
	}
	return nonNil(chatProviderModels(models)), nil
}

// DeleteProvider removes a provider and retargets open idle tabs that used it.
func (a *App) DeleteProvider(name string) error {
	return a.deleteProviderAndRetargetTabs(name)
}

// RemoveProviderAccess hides a provider from Settings > Model > Access and from
// settings model pickers. Built-in provider entries remain in the runtime config
// for back-compat, but visible defaults and idle tabs are retargeted away from
// the removed access entry when another accessed provider is available. Custom
// providers are deleted outright.
func (a *App) RemoveProviderAccess(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("remove provider access: empty provider name")
	}
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if p, ok := cfg.Provider(name); ok && isOfficialBuiltInProvider(*p) {
		return a.removeBuiltInProviderAccessAndRetargetTabs(name)
	}
	return a.deleteProviderAndRetargetTabs(name)
}

type providerRemovalTab struct {
	id   string
	ctrl *control.Controller
}

func providerAccessFallbackRef(c *config.Config, name string) string {
	name = strings.TrimSpace(name)
	for _, candidate := range c.Desktop.ProviderAccess {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == name {
			continue
		}
		p, ok := c.Provider(candidate)
		if !ok || len(p.ModelList()) == 0 {
			continue
		}
		return p.Name + "/" + p.DefaultModel()
	}
	return ""
}

func retargetProviderReferences(c *config.Config, name, fallbackRef string) {
	if strings.TrimSpace(fallbackRef) == "" {
		return
	}
	if desktopModelRefsProvider(c, c.DefaultModel, name) {
		c.DefaultModel = fallbackRef
	}
	if desktopModelRefsProvider(c, c.Agent.PlannerModel, name) {
		c.Agent.PlannerModel = fallbackRef
	}
	if desktopModelRefsProvider(c, c.Agent.SubagentModel, name) {
		c.Agent.SubagentModel = fallbackRef
	}
	for skill, ref := range c.Agent.SubagentModels {
		if desktopModelRefsProvider(c, ref, name) {
			c.Agent.SubagentModels[skill] = fallbackRef
		}
	}
}

func (a *App) removeBuiltInProviderAccessAndRetargetTabs(name string) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	fallbackRef := providerAccessFallbackRef(cfg, name)

	var affected []providerRemovalTab
	if fallbackRef != "" {
		a.mu.RLock()
		for _, id := range a.orderedTabIDsLocked() {
			tab := a.tabs[id]
			if tab == nil {
				continue
			}
			ref := tab.model
			if strings.TrimSpace(ref) == "" {
				ref = cfg.DefaultModel
			}
			if !desktopModelRefsProvider(cfg, ref, name) {
				continue
			}
			if tab.Ctrl != nil && tab.Ctrl.Running() {
				a.mu.RUnlock()
				return fmt.Errorf("finish or cancel conversations using %q before removing the provider access", name)
			}
			affected = append(affected, providerRemovalTab{id: id, ctrl: tab.Ctrl})
		}
		a.mu.RUnlock()
	}

	retargetProviderReferences(cfg, name, fallbackRef)
	removeProviderAccess(cfg, name)
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if len(affected) == 0 {
		return a.rebuild()
	}
	for _, item := range affected {
		if item.ctrl != nil {
			_ = item.ctrl.Snapshot()
			item.ctrl.Close()
		}
	}

	var rebuildTabs []*WorkspaceTab
	a.mu.Lock()
	for _, item := range affected {
		tab := a.tabs[item.id]
		if tab == nil {
			continue
		}
		tab.Ctrl = nil
		tab.model = fallbackRef
		tab.Label = fallbackRef
		tab.StartupErr = ""
		tab.Ready = a.ctx == nil
		if a.ctx != nil {
			rebuildTabs = append(rebuildTabs, tab)
		}
	}
	a.saveTabsLocked()
	a.mu.Unlock()

	for _, tab := range rebuildTabs {
		go a.buildTabController(tab)
	}
	return nil
}

func (a *App) deleteProviderAndRetargetTabs(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("remove provider: empty provider name")
	}
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	fallbackRef := providerRemovalFallbackRef(cfg, name)

	var affected []providerRemovalTab
	a.mu.RLock()
	for _, id := range a.orderedTabIDsLocked() {
		tab := a.tabs[id]
		if tab == nil {
			continue
		}
		ref := tab.model
		if strings.TrimSpace(ref) == "" {
			ref = cfg.DefaultModel
		}
		if !desktopModelRefsProvider(cfg, ref, name) {
			continue
		}
		if tab.Ctrl != nil && tab.Ctrl.Running() {
			a.mu.RUnlock()
			return fmt.Errorf("finish or cancel conversations using %q before deleting the provider", name)
		}
		affected = append(affected, providerRemovalTab{id: id, ctrl: tab.Ctrl})
	}
	a.mu.RUnlock()

	if len(affected) > 0 && fallbackRef == "" {
		return fmt.Errorf("remove provider: %q is used by open tabs and no other configured provider exists", name)
	}
	if err := cfg.RemoveProvider(name); err != nil {
		return err
	}
	removeProviderAccess(cfg, name)
	if err := cfg.SaveTo(path); err != nil {
		return err
	}

	if len(affected) == 0 {
		return a.rebuild()
	}
	for _, item := range affected {
		if item.ctrl != nil {
			_ = item.ctrl.Snapshot()
			item.ctrl.Close()
		}
	}

	var rebuildTabs []*WorkspaceTab
	a.mu.Lock()
	for _, item := range affected {
		tab := a.tabs[item.id]
		if tab == nil {
			continue
		}
		tab.Ctrl = nil
		tab.model = fallbackRef
		tab.Label = fallbackRef
		tab.StartupErr = ""
		tab.Ready = a.ctx == nil
		if a.ctx != nil {
			rebuildTabs = append(rebuildTabs, tab)
		}
	}
	a.saveTabsLocked()
	a.mu.Unlock()

	for _, tab := range rebuildTabs {
		go a.buildTabController(tab)
	}
	return nil
}

// SetProviderKey writes a secret to the global credentials file under the given
// env-var name (the one a provider's api_key_env points at) and rebuilds so it
// resolves immediately.
func (a *App) SetProviderKey(apiKeyEnv, value string) error {
	if strings.TrimSpace(apiKeyEnv) == "" {
		return fmt.Errorf("this provider has no api_key_env set")
	}
	if err := upsertDotEnv(apiKeyEnv, value); err != nil {
		return err
	}
	if err := a.rebuild(); err != nil {
		return err
	}
	a.scheduleVisionProbesForKeyEnv(apiKeyEnv)
	return nil
}

// ClearProviderKey removes a provider secret from the global credentials file
// and rebuilds so the provider immediately becomes unauthenticated.
func (a *App) ClearProviderKey(apiKeyEnv string) error {
	if strings.TrimSpace(apiKeyEnv) == "" {
		return fmt.Errorf("this provider has no api_key_env set")
	}
	if err := removeDotEnv(apiKeyEnv); err != nil {
		return err
	}
	return a.rebuild()
}

// SetPermissionMode sets the writer-fallback mode (ask|allow|deny).
func (a *App) SetPermissionMode(mode string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.SetPermissionMode(mode) })
}

func (a *App) SetAutoReviewModel(ref string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			resolved, err := selectableDesktopModelRef(c, ref)
			if err != nil {
				return err
			}
			ref = resolved
		}
		return c.SetAutoReviewModel(ref)
	})
}

// AddPermissionRule appends a rule to the allow/ask/deny list.
func (a *App) AddPermissionRule(list, rule string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.AddPermissionRule(list, rule) })
}

// RemovePermissionRule drops a rule from the allow/ask/deny list.
func (a *App) RemovePermissionRule(list, rule string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		_, err := c.RemovePermissionRule(list, rule)
		return err
	})
}

// SetSandbox updates the bash sandbox mode, network egress, and write roots.
func (a *App) SetSandbox(bash string, network bool, workspaceRoot string, allowWrite []string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Sandbox.Bash = bash
		c.Sandbox.Network = network
		c.Sandbox.WorkspaceRoot = strings.TrimSpace(workspaceRoot)
		c.Sandbox.AllowWrite = trimList(allowWrite)
		return nil
	})
}

// SetNetwork updates ordinary outbound proxy settings.
func (a *App) SetNetwork(n NetworkView) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.SetNetwork(config.NetworkConfig{
			ProxyMode: n.ProxyMode,
			ProxyURL:  n.ProxyURL,
			NoProxy:   n.NoProxy,
			Proxy: config.NetworkProxyConfig{
				Type:     n.Proxy.Type,
				Server:   n.Proxy.Server,
				Port:     n.Proxy.Port,
				Username: n.Proxy.Username,
				Password: n.Proxy.Password,
			},
		})
	})
}

func (a *App) SetBotSettings(b BotSettingsView) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		c.Bot.Enabled = true
		// The automation model has one owner: Settings > Models / the Orca
		// composer. Channel settings must not overwrite it with a stale draft.
		c.Bot.PromptMode = promptModeOrca
		c.Bot.WorkspaceRoot = ""
		c.Bot.MaxSteps = b.MaxSteps
		c.Bot.DebounceMs = b.DebounceMs
		c.Bot.Allowlist = config.BotAllowlist{
			Enabled:      true,
			AllowAll:     true,
			QQUsers:      trimList(b.Allowlist.QQUsers),
			FeishuUsers:  trimList(b.Allowlist.FeishuUsers),
			WeixinUsers:  trimList(b.Allowlist.WeixinUsers),
			QQGroups:     trimList(b.Allowlist.QQGroups),
			FeishuGroups: trimList(b.Allowlist.FeishuGroups),
			WeixinGroups: trimList(b.Allowlist.WeixinGroups),
		}
		c.Bot.QQ = config.QQBotConfig{
			Enabled:      strings.TrimSpace(b.QQ.AppID) != "",
			AppID:        strings.TrimSpace(b.QQ.AppID),
			AppSecretEnv: strings.TrimSpace(b.QQ.AppSecretEnv),
			Environment:  qqEnvironmentOrDefault(b.QQ.Environment),
		}
		c.Bot.Feishu = config.FeishuBotConfig{
			Enabled:           b.Feishu.Enabled,
			Domain:            botDomainOrDefault(b.Feishu.Domain),
			AppID:             strings.TrimSpace(b.Feishu.AppID),
			AppSecretEnv:      strings.TrimSpace(b.Feishu.AppSecretEnv),
			VerificationToken: strings.TrimSpace(b.Feishu.VerificationToken),
			Mode:              strings.TrimSpace(b.Feishu.Mode),
			WebhookPort:       b.Feishu.WebhookPort,
			RequireMention:    b.Feishu.RequireMention,
		}
		c.Bot.Weixin = config.WeixinBotConfig{
			Enabled:   true,
			AccountID: strings.TrimSpace(b.Weixin.AccountID),
			TokenEnv:  strings.TrimSpace(b.Weixin.TokenEnv),
			APIBase:   strings.TrimRight(strings.TrimSpace(b.Weixin.APIBase), "/"),
		}
		c.Bot.Connections = botConnectionConfigs(b.Connections)
		return nil
	})
	if err == nil {
		a.restartDesktopBotGateway()
	}
	return err
}

func (a *App) SetBotSecret(envName, value string) error {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return fmt.Errorf("bot secret env name is empty")
	}
	if err := upsertDotEnv(envName, value); err != nil {
		return err
	}
	return nil
}

func (a *App) ClearBotSecret(envName string) error {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return fmt.Errorf("bot secret env name is empty")
	}
	return removeDotEnv(envName)
}

// SetCloseBehavior updates desktop-only window close behavior without rebuilding
// the active controller. It must stay out of provider-visible prompt/request data.
func (a *App) SetCloseBehavior(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopCloseBehavior(mode) })
}

// SetDesktopLanguage updates only the desktop UI language. It deliberately does
// not touch config.language, which the CLI/model-facing runtime uses.
func (a *App) SetDesktopLanguage(lang string) error {
	if err := a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopLanguage(lang) }); err != nil {
		return err
	}
	a.updateTrayLocale(lang)
	return nil
}

// SetTrayLocale mirrors the resolved desktop UI language into the native tray
// menu. It is runtime-only; the persisted preference remains [desktop].language.
func (a *App) SetTrayLocale(locale string) error {
	if locale != "zh" {
		locale = "en"
	}
	a.updateTrayLocale(locale)
	return nil
}

// SetDesktopAppearance updates only desktop theme preferences. It does not
// rebuild the active controller and must stay out of provider-visible requests.
func (a *App) SetDesktopAppearance(theme, style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopAppearance("light", "slate") })
}

func (a *App) SetDesktopUIScale(scale int) error {
	// V3 uses the operating system's Per-Monitor DPI scale only. Keep this old
	// bridge binding as a no-op for in-place upgrades with a cached frontend.
	return nil
}

// SetDesktopUIStyle switches the presentation layer without rebuilding the
// controller or interrupting an active turn.
func (a *App) SetDesktopUIStyle(style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopUIStyle(style) })
}

// SetDesktopCheckUpdates updates only the desktop startup update-check
// preference. Manual checks in Settings are unaffected.
func (a *App) SetDesktopCheckUpdates(enabled bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopCheckUpdates(enabled) })
}

func (a *App) SetAutomationFullAccess(enabled bool) error {
	if err := a.applyConfigOnly(func(c *config.Config) error {
		c.Desktop.AutomationFullAccess = enabled
		return nil
	}); err != nil {
		return err
	}
	mode := control.ToolApprovalAsk
	if enabled {
		mode = control.ToolApprovalYolo
	}
	a.mu.Lock()
	for _, tab := range a.tabs {
		if tab == nil || tab.Scope != scopeAutomation || tab.ReadOnly {
			continue
		}
		tab.toolApprovalMode = mode
		if tab.Ctrl != nil {
			tab.Ctrl.SetTrustedAutomationAccess(enabled)
		}
	}
	a.saveTabsLocked()
	a.mu.Unlock()
	a.restartDesktopBotGatewayWhenIdle()
	a.emitReady(a.ctx)
	return nil
}

func (a *App) SetAutomationModel(modelRef string) error {
	modelRef = strings.TrimSpace(modelRef)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	entry, ok := cfg.ResolveModel(modelRef)
	if !ok || !modelProviderAccessAllowed(providerAccessSet(cfg.Desktop.ProviderAccess), entry.Name) {
		return fmt.Errorf("unknown or unavailable automation model %q", modelRef)
	}
	modelRef = entry.Name + "/" + entry.Model
	if err := a.applyConfigOnly(func(c *config.Config) error {
		c.Bot.Model = modelRef
		return nil
	}); err != nil {
		return err
	}

	type automationTabModelChange struct {
		tab     *WorkspaceTab
		ctrl    *control.Controller
		running bool
	}
	var tabs []automationTabModelChange
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab == nil || tab.Scope != scopeAutomation || tab.ReadOnly {
			continue
		}
		ctrl := tab.Ctrl
		tabs = append(tabs, automationTabModelChange{tab: tab, ctrl: ctrl, running: ctrl != nil && ctrl.Running()})
	}
	a.mu.RUnlock()
	for _, item := range tabs {
		if item.running {
			go a.rebuildAutomationTabModelWhenIdle(item.tab, modelRef, item.ctrl)
			continue
		}
		a.rebuildAutomationTabModel(item.tab, modelRef, item.ctrl)
	}
	a.restartDesktopBotGatewayWhenIdle()
	return nil
}

func (a *App) rebuildAutomationTabModelWhenIdle(tab *WorkspaceTab, modelRef string, ctrl *control.Controller) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctrl.Running() {
				continue
			}
			cfg, err := config.Load()
			if err != nil {
				return
			}
			latest, _, ok := cfg.ResolveModelWithFallback(cfg.Bot.Model)
			if !ok || latest != modelRef {
				return
			}
			a.rebuildAutomationTabModel(tab, modelRef, ctrl)
			return
		case <-a.bootContext().Done():
			return
		}
	}
}

func (a *App) rebuildAutomationTabModel(tab *WorkspaceTab, modelRef string, expected *control.Controller) {
	if tab == nil {
		return
	}
	generation := a.beginTabRuntimeReconfigure(tab)
	tab.runtimeMu.Lock()
	defer tab.runtimeMu.Unlock()
	if !a.tabRuntimeGenerationCurrent(tab, generation) {
		return
	}

	a.mu.RLock()
	valid := a.tabs[tab.ID] == tab && tab.Scope == scopeAutomation && !tab.ReadOnly && tab.Ctrl == expected && (tab.Ctrl == nil || !tab.Ctrl.Running())
	a.mu.RUnlock()
	if !valid {
		a.finishTabRuntimeReconfigure(tab, generation, false)
		return
	}

	success := false
	defer func() { a.finishTabRuntimeReconfigure(tab, generation, success) }()
	cfg, err := config.LoadForRoot(tab.WorkspaceRoot)
	if err != nil {
		a.noticeForTab(tab.ID, fmt.Sprintf("could not switch Orca model: %v", err))
		return
	}
	resolved, _, ok := cfg.ResolveModelWithFallback(modelRef)
	if !ok || resolved != modelRef {
		a.noticeForTab(tab.ID, fmt.Sprintf("could not switch Orca model: model %q is unavailable", modelRef))
		return
	}

	oldCtrl := tab.Ctrl
	var carried []provider.Message
	prevPath := ""
	if oldCtrl != nil {
		prevPath = oldCtrl.SessionPath()
		_ = oldCtrl.Snapshot()
		carried = oldCtrl.History()
	}

	assistantStoreDir := ""
	if store, storeErr := memory.EnsureCanonicalAssistantStore(config.MemoryUserDir()); storeErr == nil {
		assistantStoreDir = store.Dir
	} else {
		a.noticeForTab(tab.ID, fmt.Sprintf("could not prepare shared assistant profile: %v", storeErr))
	}
	var extraTools []tool.Tool
	var turnContext func() string
	if a.conversationBroker != nil {
		extraTools = a.conversationBroker.Tools(tab.ID, tab.TopicID)
		turnContext = func() string { return a.conversationBroker.Index(tab.TopicID) }
	}
	extraTools = append(extraTools, automationHistoryTool{
		topicID:     tab.TopicID,
		currentPath: func() string { return tab.currentSessionPath() },
	})

	newCtrl, err := a.buildController(a.bootContext(), boot.Options{
		Model:                   modelRef,
		RequireKey:              false,
		Sink:                    tab.sink,
		WorkspaceRoot:           tab.WorkspaceRoot,
		SessionDir:              tabSessionDir(tab),
		EffortOverride:          cloneStringPtr(tab.effort),
		RuntimeProfile:          promptModeOrca,
		MemoryProfile:           memory.ProfileAssistant,
		AssistantMemoryStoreDir: assistantStoreDir,
		ExtraTools:              extraTools,
		TurnContext:             turnContext,
		TurnLease:               a.sessionGate.Acquire,
		RefreshOnLease:          true,
	}, tab.ID)
	if err != nil {
		a.noticeForTab(tab.ID, fmt.Sprintf("could not switch Orca model: %v", err))
		return
	}
	a.bindControllerDisplayRecorder(newCtrl)
	newCtrl.EnableInteractiveApproval()
	applyTabModeToController(newCtrl, tab.mode)
	applyTabToolApprovalModeToController(newCtrl, tab.toolApprovalMode)
	newCtrl.SetTrustedAutomationAccess(cfg.Desktop.AutomationFullAccess)
	newCtrl.SetAskWorkflow(tab.askWorkflow)
	newCtrl.SetStepThinking(tab.stepThinking)
	newCtrl.SetGoal(tab.goal)
	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	resumeWithControllerSystem(newCtrl, carried, path)

	a.mu.Lock()
	if current := a.tabs[tab.ID]; current != tab || tab.runtimeGeneration != generation || tab.Ctrl != expected {
		a.mu.Unlock()
		newCtrl.Close()
		return
	}
	tab.Ctrl = newCtrl
	tab.model = modelRef
	tab.Label = newCtrl.Label()
	tab.StartupErr = ""
	tab.Ready = true
	a.rememberConversationPrefsLocked(tab)
	a.saveTabsLocked()
	a.mu.Unlock()
	if oldCtrl != nil {
		oldCtrl.Close()
	}
	a.persistTabSessionPath(tab, path)
	success = true
}

// SetExpandThinking sets whether reasoning text is expanded by default on
// the desktop. It is desktop-only and does not rebuild the controller.
func (a *App) SetExpandThinking(on bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetExpandThinking(on) })
}

// SetProcessDisplayMode updates the desktop-only process presentation without
// rebuilding the model controller.
func (a *App) SetProcessDisplayMode(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetProcessDisplayMode(mode) })
}

// SetActivityIndicatorEnabled toggles the optional process activity animation.
// It is UI-only and does not rebuild the model controller.
func (a *App) SetActivityIndicatorEnabled(enabled bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetActivityIndicatorEnabled(enabled) })
}

// SetVisionEnabled rebuilds controllers because the setting changes both the
// provider-visible policy and whether image parts are attached to new turns.
func (a *App) SetVisionEnabled(enabled bool) error {
	mode := config.VisionModeOff
	if enabled {
		mode = config.VisionModeOn
	}
	return a.SetVisionMode(mode)
}

func (a *App) SetVisionMode(mode string) error {
	if err := a.applyConfigOnly(func(c *config.Config) error { return c.SetVisionMode(mode) }); err != nil {
		return err
	}
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil || !tab.Ctrl.Running() {
		return a.rebuild()
	}
	ctrl := tab.Ctrl
	var appDone <-chan struct{}
	if a.ctx != nil {
		appDone = a.ctx.Done()
	}
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if ctrl.Running() {
					continue
				}
				a.mu.RLock()
				stillActive := a.activeTabLocked() == tab && tab.Ctrl == ctrl
				a.mu.RUnlock()
				if stillActive {
					if err := a.rebuild(); err != nil {
						a.noticeForTab(tab.ID, "多模态识图设置已保存，但应用到当前对话失败："+err.Error())
					}
				}
				return
			case <-appDone:
				return
			}
		}
	}()
	return nil
}

// MigrateDesktopPreferences imports old browser-local desktop preferences into
// the user config once. Existing [desktop] values win so stale localStorage never
// overwrites an explicit config edit.
func (a *App) MigrateDesktopPreferences(language, theme, style string) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		if strings.TrimSpace(c.Desktop.Language) == "" {
			if err := c.SetDesktopLanguage(language); err != nil {
				return err
			}
		}
		if strings.TrimSpace(c.Desktop.Theme) == "" && strings.TrimSpace(c.Desktop.ThemeStyle) == "" {
			if err := c.SetDesktopAppearance("light", "slate"); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetAgentParams updates sampling temperature, optional step guards, and the
// base system prompt.
func (a *App) SetAgentParams(temperature float64, maxSteps int, plannerMaxSteps int, softCompactRatio float64, compactRatio float64, compactForceRatio float64, systemPrompt string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Agent.Temperature = temperature
		c.Agent.MaxSteps = maxSteps
		c.Agent.PlannerMaxSteps = plannerMaxSteps
		softCompactRatio, compactRatio, compactForceRatio = normalizeCompactRatios(softCompactRatio, compactRatio, compactForceRatio)
		c.Agent.SoftCompactRatio = softCompactRatio
		c.Agent.CompactRatio = compactRatio
		c.Agent.CompactForceRatio = compactForceRatio
		c.Agent.SystemPrompt = systemPrompt
		return nil
	})
}

func compactRatioOrDefault(v, def float64) float64 {
	if v <= 0 {
		return def
	}
	return v
}

func normalizeCompactRatios(soft, trigger, force float64) (float64, float64, float64) {
	soft = clampFloat(compactRatioOrDefault(soft, 0.5), 0.1, 0.85)
	trigger = clampFloat(compactRatioOrDefault(trigger, 0.8), 0.2, 0.95)
	force = clampFloat(compactRatioOrDefault(force, 0.9), 0.3, 0.98)
	if soft >= trigger {
		soft = clampFloat(trigger-0.1, 0.1, 0.85)
	}
	if force <= trigger {
		force = clampFloat(trigger+0.05, 0.3, 0.98)
	}
	if soft >= trigger {
		soft = 0.5
		trigger = 0.8
		force = 0.9
	}
	return soft, trigger, force
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// trimList drops blank entries from a string slice (and returns a non-nil slice).
func trimList(in []string) []string {
	out := []string{}
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
