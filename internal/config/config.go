// Package config loads O.R.C.A's runtime configuration from TOML. Resolution order:
// flag > project ./deepseek-orca.toml > user ~/.config/orca/config.toml > built-in defaults.
// Secrets come from the environment via api_key_env and are never stored in
// config files.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/billing"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/product"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

var validSkillName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// IsValidSkillName reports whether name is a usable skill identifier.
func IsValidSkillName(name string) bool { return validSkillName.MatchString(name) }

// SkillNameKey normalizes a skill identifier for config comparisons.
func SkillNameKey(name string) string {
	name = strings.TrimSpace(name)
	if !IsValidSkillName(name) {
		return ""
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

// Config is O.R.C.A's runtime configuration.
type Config struct {
	ConfigVersion int                 `toml:"config_version"`
	DefaultModel  string              `toml:"default_model"`
	Language      string              `toml:"language"` // ui/model language tag (e.g. "zh"); empty = auto-detect from $LANG / $DEEPSEEK_ORCA_LANG
	UI            UIConfig            `toml:"ui"`
	Desktop       DesktopConfig       `toml:"desktop"`
	Notifications NotificationsConfig `toml:"notifications"`
	Agent         AgentConfig         `toml:"agent"`
	Providers     []ProviderEntry     `toml:"providers"`
	Tools         ToolsConfig         `toml:"tools"`
	ToolLibrary   ToolLibraryConfig   `toml:"tool_library"`
	Permissions   PermissionsConfig   `toml:"permissions"`
	Sandbox       SandboxConfig       `toml:"sandbox"`
	Network       NetworkConfig       `toml:"network"`
	Plugins       []PluginEntry       `toml:"plugins"`
	Skills        SkillsConfig        `toml:"skills"`
	Codegraph     CodegraphConfig     `toml:"codegraph"`
	Statusline    StatuslineConfig    `toml:"statusline"`
	LSP           LSPConfig           `toml:"lsp"`
	Bot           BotConfig           `toml:"bot"`
	LocalAI       LocalAIConfig       `toml:"local_ai"`

	// env is scoped to this loaded workspace. It is deliberately private so
	// project .env values do not become process-wide state.
	env map[string]string
}

// Env looks up an environment value with host-process variables taking
// precedence over this config's workspace-scoped dotenv values. It never
// mutates the host environment.
func (c *Config) Env(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	var scope map[string]string
	if c != nil {
		scope = c.env
	}
	return lookupScopedEnv(scope, key)
}

// Environment returns a copy of the host environment merged with this config's
// dotenv values. Existing host entries are preserved, and project values are
// added only when the host has no entry. The returned map is independent and
// changing it does not mutate the host.
func (c *Config) Environment() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	if c != nil {
		seen := make(map[string]struct{}, len(env))
		for key := range env {
			seen[environmentKey(key)] = struct{}{}
		}
		for key, value := range c.env {
			if _, hostSet := seen[environmentKey(key)]; !hostSet {
				env[key] = value
			}
		}
	}
	return env
}

func environmentKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(key)
	}
	return key
}

func lookupScopedEnv(scope map[string]string, key string) (string, bool) {
	if value, ok := os.LookupEnv(key); ok {
		return value, true
	}
	if value, ok := scope[key]; ok {
		return value, true
	}
	if runtime.GOOS == "windows" {
		for scopedKey, value := range scope {
			if strings.EqualFold(scopedKey, key) {
				return value, true
			}
		}
	}
	return "", false
}

// EnvironmentSlice returns Environment in exec.Cmd.Env format, with stable
// ordering for reproducible subprocess setup.
func (c *Config) EnvironmentSlice() []string {
	env := c.Environment()
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

// UIConfig controls CLI presentation-only settings. Desktop appearance is kept in
// DesktopConfig so desktop preferences cannot alter terminal output or prompts.
type UIConfig struct {
	Theme          string `toml:"theme"`           // auto|dark|light; empty resolves to auto
	ThemeStyle     string `toml:"theme_style"`     // slate; legacy aliases normalize to the default style
	ShortcutLayout string `toml:"shortcut_layout"` // classic|desktop; accepted for compatibility
	CloseBehavior  string `toml:"close_behavior"`  // legacy desktop close behavior; prefer desktop.close_behavior
	ShowReasoning  bool   `toml:"show_reasoning"`  // Ctrl+O / /verbose: show thinking text in CLI; false = collapsed
}

// DesktopConfig controls desktop-only UI preferences. It is intentionally
// separate from top-level language and [ui] so desktop choices do not affect CLI
// language, terminal colours, or provider-visible prompt/request data.
type DesktopConfig struct {
	Language              string   `toml:"language"`                          // auto|en|zh; empty/auto = browser/OS auto-detect
	Theme                 string   `toml:"theme"`                             // desktop is fixed to light; legacy values are ignored
	ThemeStyle            string   `toml:"theme_style"`                       // desktop is fixed to slate; legacy values are ignored
	UIStyle               string   `toml:"ui_style"`                          // modern|classic; presentation-only and applied on restart
	CloseBehavior         string   `toml:"close_behavior"`                    // quit|background; desktop window close behavior
	CheckUpdates          *bool    `toml:"check_updates"`                     // startup update checks; nil keeps the default enabled
	ProviderAccess        []string `toml:"provider_access"`                   // desktop-only list of provider entries shown in Settings > Model > Access
	ExpandThinking        bool     `toml:"expand_thinking"`                   // true = show reasoning text expanded by default; false = collapsed
	ProcessDisplayMode    string   `toml:"process_display_mode"`              // compact|detailed; standard is accepted as a legacy alias for compact
	ShowReasoning         *bool    `toml:"show_reasoning,omitempty"`          // display only; legacy process layout preferences never hide progress
	ActivityIndicator     bool     `toml:"activity_indicator_enabled"`        // show the optional animated process activity mark
	VisionEnabled         bool     `toml:"vision_enabled"`                    // send attached image bytes to the selected model
	VisionMode            string   `toml:"vision_mode"`                       // off|auto|on; vision_enabled is retained for legacy configs
	UIScale               int      `toml:"ui_scale"`                          // legacy V2 field; V3 reads and ignores it
	AssistantAutoMemory   *bool    `toml:"assistant_auto_memory_enabled"`     // assistant-mode silent profile memory updates; nil = enabled
	AssistantMemoryRecall *bool    `toml:"assistant_memory_recall_enabled"`   // inject assistant memories before assistant-mode turns; nil = enabled
	AutomationFullAccess  bool     `toml:"automation_full_access_approved"`   // one-time consent for trusted automation turns
	OnboardingCompleted   bool     `toml:"onboarding_completed"`              // generic V3 provider/local-model onboarding has been acknowledged
	ComputerControlModel  string   `toml:"computer_control_model"`            // fully-qualified provider/model ref used by Computer Use
	ComputerUseFullAccess bool     `toml:"computer_use_full_access_approved"` // persistent one-time Computer Use consent
	ComputerUseConsent    int      `toml:"computer_use_consent_version"`      // versioned scope of the persisted consent
	ComputerDebugCapture  bool     `toml:"computer_debug_capture"`            // persist diagnostic screenshots; disabled by default
	ConversationMode      string   `toml:"conversation_mode"`                 // coding|assistant; latest choice for new conversations
}

// LocalAIConfig controls the optional O.R.C.A-managed llama.cpp sidecar. The
// actual installed runtime, model files and resumable downloads are tracked by
// manifests under the local data root rather than trusted from TOML alone.
type LocalAIConfig struct {
	Enabled           bool   `toml:"enabled"`
	RuntimeVersion    string `toml:"runtime_version"`
	ModelsDir         string `toml:"models_dir"`
	IdleUnloadMinutes int    `toml:"idle_unload_minutes"`
	VRAMReserveMiB    int    `toml:"vram_reserve_mib"`
}

func (c *Config) LocalAIIdleUnloadMinutes() int {
	if c == nil || c.LocalAI.IdleUnloadMinutes < 0 {
		return 10
	}
	return c.LocalAI.IdleUnloadMinutes
}

func (c *Config) LocalAIVRAMReserveMiB() int {
	if c == nil || c.LocalAI.VRAMReserveMiB < 512 {
		return 2048
	}
	return c.LocalAI.VRAMReserveMiB
}

const (
	ProcessDisplayCompact  = "compact"
	ProcessDisplayStandard = "standard"
	ProcessDisplayDetailed = "detailed"
)

const (
	DesktopUIStyleModern  = "modern"
	DesktopUIStyleClassic = "classic"
)

const (
	VisionModeOff  = "off"
	VisionModeAuto = "auto"
	VisionModeOn   = "on"
)

func (c *Config) DesktopVisionMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.VisionMode)) {
	case VisionModeOff:
		return VisionModeOff
	case VisionModeOn:
		return VisionModeOn
	default:
		return VisionModeAuto
	}
}

func (c *Config) DesktopUIScale() int {
	return 0
}

// DesktopUIStyle controls only the desktop presentation layer. Empty and
// unknown values intentionally select the modern V3 shell.
func (c *Config) DesktopUIStyle() string {
	if c != nil && strings.EqualFold(strings.TrimSpace(c.Desktop.UIStyle), DesktopUIStyleClassic) {
		return DesktopUIStyleClassic
	}
	return DesktopUIStyleModern
}

// The public two-state control now changes reasoning visibility only.
func (c *Config) DesktopProcessDisplayMode() string {
	if c.Desktop.ShowReasoning != nil && *c.Desktop.ShowReasoning {
		return ProcessDisplayDetailed
	}
	return ProcessDisplayCompact
}

// NotificationsConfig controls optional system notifications for CLI chat/run.
type NotificationsConfig struct {
	Enabled         bool `toml:"enabled"`
	TurnDone        bool `toml:"turn_done"`
	ApprovalRequest bool `toml:"approval_request"`
	AskRequest      bool `toml:"ask_request"`
}

// UITheme normalizes ui.theme to a supported value.
func (c *Config) UITheme() string {
	switch strings.ToLower(strings.TrimSpace(c.UI.Theme)) {
	case "dark":
		return "dark"
	case "light":
		return "light"
	default:
		return "auto"
	}
}

// UIThemeStyle normalizes ui.theme_style. Empty means "pick the default style
// for the resolved light/dark shell".
func (c *Config) UIThemeStyle() string {
	return normalizeThemeStyle(c.UI.ThemeStyle)
}

// UIShortcutLayout normalizes the legacy CLI shortcut layout setting. It is kept
// for compatibility; Shift+Tab toggles Plan and Ctrl+Y toggles YOLO in both
// layouts.
func (c *Config) UIShortcutLayout() string {
	switch strings.ToLower(strings.TrimSpace(c.UI.ShortcutLayout)) {
	case "desktop", "dual", "dual-axis", "dual_axis":
		return "desktop"
	default:
		return "classic"
	}
}

func normalizeThemeStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "slate", "graphite", "midnight", "sandstone", "porcelain", "linen", "glacier", "nocturne", "aurora", "carbon", "pop", "pop-paint", "poppaint", "amber", "ember":
		return "slate"
	default:
		return ""
	}
}

func normalizeCloseBehavior(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "quit", "exit":
		return "quit"
	default:
		return "background"
	}
}

// DesktopLanguage normalizes the desktop UI language. Empty means auto-detect
// from the browser/OS locale; it deliberately does not read top-level language,
// which is used by the CLI/model-facing runtime.
func (c *Config) DesktopLanguage() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.Language)) {
	case "en":
		return "en"
	case "zh":
		return "zh"
	default:
		return ""
	}
}

// DesktopTheme normalizes desktop.theme. The desktop shell now uses one fixed
// DeepSeek-Orca product look, so legacy auto/dark values are ignored.
func (c *Config) DesktopTheme() string {
	return "light"
}

// DesktopThemeStyle normalizes desktop.theme_style to the single supported
// visual direction.
func (c *Config) DesktopThemeStyle() string {
	return "slate"
}

// DesktopCloseBehavior normalizes the desktop close-window preference. It falls
// back to the legacy ui.close_behavior value for configs written before [desktop]
// existed.
func (c *Config) DesktopCloseBehavior() string {
	if strings.TrimSpace(c.Desktop.CloseBehavior) != "" {
		return normalizeCloseBehavior(c.Desktop.CloseBehavior)
	}
	return normalizeCloseBehavior(c.UI.CloseBehavior)
}

// UICloseBehavior is the legacy name for DesktopCloseBehavior.
func (c *Config) UICloseBehavior() string {
	return c.DesktopCloseBehavior()
}

// DesktopCheckUpdates reports whether the desktop should check for updates.
func (c *Config) DesktopCheckUpdates() bool {
	if c == nil || c.Desktop.CheckUpdates == nil {
		return true
	}
	return *c.Desktop.CheckUpdates
}

func (c *Config) DesktopAssistantAutoMemoryEnabled() bool {
	if c == nil || c.Desktop.AssistantAutoMemory == nil {
		return true
	}
	return *c.Desktop.AssistantAutoMemory
}

func (c *Config) DesktopAssistantMemoryRecallEnabled() bool {
	if c == nil || c.Desktop.AssistantMemoryRecall == nil {
		return true
	}
	return *c.Desktop.AssistantMemoryRecall
}

// LSPConfig governs the optional Language Server Protocol tools (lsp_definition,
// lsp_references, lsp_hover, lsp_diagnostics). Enabled defaults to true; the
// servers themselves are never bundled - each resolves on PATH and the tool
// returns an install hint when it is missing, so the capability is dormant until
// the user installs a server. Servers overrides or extends the built-in language
// -> server map, keyed by language id (e.g. "go", "rust", "python").
type LSPConfig struct {
	Enabled bool                 `toml:"enabled"`
	Servers map[string]LSPServer `toml:"servers"`
}

// LSPServer overrides a built-in language's server or, when keyed by a new
// language, adds one. An empty field falls back to the built-in default for that
// language; Extensions is required when adding a language the built-ins don't
// cover (e.g. ".ex" for Elixir) so files route to it.
type LSPServer struct {
	Command     string            `toml:"command"`
	Args        []string          `toml:"args"`
	Env         map[string]string `toml:"env"`
	LanguageID  string            `toml:"language_id"`
	Extensions  []string          `toml:"extensions"`
	InstallHint string            `toml:"install_hint"`
}

// StatuslineConfig configures a custom status line. Command, when set, is run at
// startup and after each turn; its first line of stdout replaces the built-in
// status data row. A JSON payload (model, context tokens, cwd) is fed on stdin.
type StatuslineConfig struct {
	Command string `toml:"command"`
}

// CodegraphConfig governs the built-in CodeGraph MCP server - symbol/call-graph
// code intelligence (tree-sitter + SQLite) that gives the agent codegraph_*
// search / context / explore / trace / node tools. Enabled defaults to true so
// upgrades keep it for existing configs; first-run scaffolds write enabled =
// false so only brand-new users start without it. AutoInstall (default true)
// lets deepseek-orca fetch the CodeGraph runtime into its cache when CodeGraph is
// enabled but missing; set false to require an explicit `deepseek-orca codegraph
// install` (e.g. for air-gapped or headless runs). Path overrides binary
// resolution; empty resolves the cache, then a `codegraph` on PATH, then a
// bundle beside the executable. CodeGraph always starts in the background when
// enabled; legacy tier values are ignored and removed during config load.
type CodegraphConfig struct {
	Enabled     bool   `toml:"enabled"`
	AutoInstall bool   `toml:"auto_install"`
	Path        string `toml:"path"`
	Tier        string `toml:"tier"`
}

func (c CodegraphConfig) ShouldAutoStart() bool {
	return c.Enabled
}

func (c CodegraphConfig) ResolvedTier() string {
	return "background"
}

// BotConfig controls the multi-channel IM bot gateway.
type BotConfig struct {
	Enabled       bool                  `toml:"enabled"`
	Model         string                `toml:"model"`       // empty = default_model
	PromptMode    string                `toml:"prompt_mode"` // orca; legacy values are migrated on load
	WorkspaceRoot string                `toml:"workspace_root"`
	MaxSteps      int                   `toml:"max_steps"`
	DebounceMs    int                   `toml:"debounce_ms"`
	Allowlist     BotAllowlist          `toml:"allowlist"`
	QQ            QQBotConfig           `toml:"qq"`
	Feishu        FeishuBotConfig       `toml:"feishu"`
	Weixin        WeixinBotConfig       `toml:"weixin"`
	Connections   []BotConnectionConfig `toml:"connections"`
}

// BotAllowlist restricts which remote users/groups may invoke the bot.
type BotAllowlist struct {
	Enabled      bool     `toml:"enabled"`
	AllowAll     bool     `toml:"allow_all"`
	QQUsers      []string `toml:"qq_users"`
	FeishuUsers  []string `toml:"feishu_users"`
	WeixinUsers  []string `toml:"weixin_users"`
	QQGroups     []string `toml:"qq_groups"`
	FeishuGroups []string `toml:"feishu_groups"`
	WeixinGroups []string `toml:"weixin_groups"`
}

// QQBotConfig configures QQ official Bot API v2.
type QQBotConfig struct {
	Enabled      bool   `toml:"enabled"`
	AppID        string `toml:"app_id"`
	AppSecretEnv string `toml:"app_secret_env"` // e.g. QQ_BOT_APP_SECRET
	Environment  string `toml:"environment"`    // sandbox|production
}

// FeishuBotConfig configures Feishu/Lark custom app bots.
type FeishuBotConfig struct {
	Enabled           bool   `toml:"enabled"`
	Domain            string `toml:"domain"` // feishu|lark
	AppID             string `toml:"app_id"`
	AppSecretEnv      string `toml:"app_secret_env"`     // e.g. FEISHU_BOT_APP_SECRET
	VerificationToken string `toml:"verification_token"` // webhook challenge token
	Mode              string `toml:"mode"`               // webhook|websocket
	WebhookPort       int    `toml:"webhook_port"`
	RequireMention    bool   `toml:"require_mention"`
}

// WeixinBotConfig configures WeChat iLink bot access.
type WeixinBotConfig struct {
	Enabled   bool   `toml:"enabled"`
	AccountID string `toml:"account_id"`
	TokenEnv  string `toml:"token_env"` // e.g. WEIXIN_BOT_TOKEN
	APIBase   string `toml:"api_base"`  // iLink API base URL
}

// BotConnectionConfig is the desktop-friendly connection record for IM bot
// channels. It keeps install/runtime state separate from legacy per-provider
// knobs so the UI can expose a simple "connect first" flow while old configs
// keep working.
type BotConnectionConfig struct {
	ID              string                        `toml:"id"`
	Provider        string                        `toml:"provider"` // qq|feishu|weixin
	Domain          string                        `toml:"domain"`   // feishu|lark|weixin|qq
	Label           string                        `toml:"label"`
	Enabled         bool                          `toml:"enabled"`
	Status          string                        `toml:"status"` // disconnected|pending|connected|error
	Credential      BotConnectionCredential       `toml:"credential"`
	SessionMappings []BotConnectionSessionMapping `toml:"session_mappings"`
	GuideSent       bool                          `toml:"guide_sent"`
	LastError       string                        `toml:"last_error"`
	CreatedAt       string                        `toml:"created_at"`
	UpdatedAt       string                        `toml:"updated_at"`
}

type BotConnectionCredential struct {
	AppID        string `toml:"app_id"`
	AppSecretEnv string `toml:"app_secret_env"`
	AccountID    string `toml:"account_id"`
	TokenEnv     string `toml:"token_env"`
	Environment  string `toml:"environment"`
}

type BotConnectionSessionMapping struct {
	RemoteID  string `toml:"remote_id"`
	SessionID string `toml:"session_id"`
	UpdatedAt string `toml:"updated_at"`
}

// NetworkConfig controls ordinary outbound HTTP traffic such as model providers,
// wallet-balance lookups, updater checks, CodeGraph downloads, and web_fetch.
// web_fetch reuses these proxy settings while keeping its own SSRF-guarded
// dialer.
type NetworkConfig struct {
	// ProxyMode is "auto" (default; environment proxy for now), "env", "custom",
	// or "off". auto leaves room for OS proxy detection later without changing the
	// config shape.
	ProxyMode string `toml:"proxy_mode"`
	// ProxyURL is an advanced custom override such as "socks5://127.0.0.1:7890".
	// When set and proxy_mode = "custom", it wins over the structured proxy table.
	ProxyURL string `toml:"proxy_url"`
	// NoProxy is honored for custom proxies. Env/auto modes use NO_PROXY from the
	// process environment instead.
	NoProxy string             `toml:"no_proxy"`
	Proxy   NetworkProxyConfig `toml:"proxy"`
}

// NetworkProxyConfig is the structured custom-proxy editor shape. Password is
// optional and supports ${VAR} expansion, so users can avoid storing it literally.
type NetworkProxyConfig struct {
	Type     string `toml:"type"` // http|https|socks5|socks5h
	Server   string `toml:"server"`
	Port     int    `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

// NetworkProxySpec returns the expanded proxy settings used by netclient.
func (c *Config) NetworkProxySpec() netclient.ProxySpec {
	return netclient.ProxySpec{
		Mode:        c.Network.ProxyMode,
		URL:         ExpandVars(c.Network.ProxyURL),
		NoProxy:     ExpandVars(c.Network.NoProxy),
		Type:        c.Network.Proxy.Type,
		Server:      ExpandVars(c.Network.Proxy.Server),
		Port:        c.Network.Proxy.Port,
		Username:    ExpandVars(c.Network.Proxy.Username),
		Password:    ExpandVars(c.Network.Proxy.Password),
		DirectHosts: c.directProxyHosts(),
	}
}

// directProxyHosts collects the base_url hosts of providers marked no_proxy, so
// netclient bypasses the proxy for them without knowing any provider by name.
//
// Only for an auto-detected proxy (auto/env): that proxy is typically a
// GFW-circumvention one not meant for domestic endpoints (e.g. mimo), so keep
// them direct. An explicit proxy_mode = "custom" is the user saying "route
// everything through this" - e.g. a mandatory corporate proxy - so honor it for
// every provider; a custom-proxy user who wants a host direct uses
// network.no_proxy instead (#3635).
func (c *Config) directProxyHosts() []string {
	if c.NetworkProxyMode() == netclient.ModeCustom {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range c.Providers {
		if !p.NoProxy {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(p.BaseURL))
		if err != nil {
			continue
		}
		if h := u.Hostname(); h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// NetworkProxyMode normalizes network.proxy_mode to a known value.
func (c *Config) NetworkProxyMode() string {
	return netclient.NormalizeMode(c.Network.ProxyMode)
}

// SkillsConfig configures skill discovery. Paths adds extra "custom"-scope skill
// roots - each a directory of SKILL.md / <name>.md playbooks - scanned between
// the project roots (.orca/.agents/.agent/.claude under the workspace) and
// the global roots. ExcludedPaths hides matching discovery roots without deleting
// folders. ~, relative paths, and ${VAR} expansion are supported. DisabledSkills
// hides named skills from the agent prompt, slash invocation, and skill tools
// while keeping them manageable.
type SkillsConfig struct {
	Paths          []string `toml:"paths"`
	ExcludedPaths  []string `toml:"excluded_paths"`
	DisabledSkills []string `toml:"disabled_skills"`
	MaxDepth       int      `toml:"max_depth"`
}

// SkillCustomPaths returns the configured custom skill roots with ${VAR}
// expanded; empty entries are dropped.
func (c *Config) SkillCustomPaths() []string {
	var out []string
	for _, p := range c.Skills.Paths {
		if p = ExpandVars(p); strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// SkillExcludedPaths returns configured skill roots that should be hidden from
// discovery, with ${VAR} expanded and empty entries dropped.
func (c *Config) SkillExcludedPaths() []string {
	var out []string
	for _, p := range c.Skills.ExcludedPaths {
		if p = ExpandVars(p); strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// SkillMaxDepth bounds nested skill discovery. Depth 3 favors bundled skill
// packs while Store keeps nested markdown safe by requiring descriptions.
func (c *Config) SkillMaxDepth() int {
	const (
		defaultDepth = 3
		maxDepth     = 5
	)
	if c == nil || c.Skills.MaxDepth == 0 {
		return defaultDepth
	}
	if c.Skills.MaxDepth < 1 {
		return 1
	}
	if c.Skills.MaxDepth > maxDepth {
		return maxDepth
	}
	return c.Skills.MaxDepth
}

// DisabledSkillNames returns valid disabled skill identifiers, preserving the
// first spelling and dropping duplicates/empty entries.
func (c *Config) DisabledSkillNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range c.Skills.DisabledSkills {
		name = strings.TrimSpace(name)
		if !IsValidSkillName(name) {
			continue
		}
		key := SkillNameKey(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// IsSkillDisabled reports whether name is configured as disabled.
func (c *Config) IsSkillDisabled(name string) bool {
	key := SkillNameKey(name)
	if key == "" {
		return false
	}
	for _, disabled := range c.DisabledSkillNames() {
		if SkillNameKey(disabled) == key {
			return true
		}
	}
	return false
}

// SandboxConfig bounds the blast radius of tool calls (Phase 0: file-writer
// confinement). WorkspaceRoot is the directory the built-in file writers
// (write_file / edit_file / multi_edit) may modify; empty means the current
// working directory, so writes stay inside the project by default. AllowWrite
// lists extra directories writers may also touch (e.g. a sibling repo or a temp
// dir). Both support ${VAR} / ${VAR:-default} expansion. Reads are unrestricted;
// confining `bash` is Phase 1 (OS-level sandbox).
type SandboxConfig struct {
	WorkspaceRoot string   `toml:"workspace_root"`
	AllowWrite    []string `toml:"allow_write"`
	// Bash is the OS-sandbox mode for the bash tool: "enforce" (default) jails
	// each command, "off" runs it unconfined. Phase 1; macOS only for now, with
	// a graceful fallback elsewhere (see internal/sandbox).
	Bash string `toml:"bash"`
	// Network allows network egress from inside the bash sandbox. Defaults true
	// so module/package downloads keep working; the boundary is then writes.
	Network bool `toml:"network"`
}

// WriteRoots returns the directories file-writer tools may modify: the
// workspace root (defaulting to the current working directory when unset) plus
// any AllowWrite extras, with ${VAR} expanded. The roots are returned as given
// (relative or absolute); the confiner resolves them to absolute, symlink-free
// paths. The result is always non-empty, so confinement is on by default.
func (c *Config) WriteRoots() []string {
	return c.WriteRootsForRoot(".")
}

// WriteRootsForRoot is like WriteRoots but falls back to fallbackRoot when the
// config doesn't explicitly set a workspace_root. Desktop tabs pass their
// project root here so tool confinement is correct without changing cwd.
func (c *Config) WriteRootsForRoot(fallbackRoot string) []string {
	root := ExpandVars(c.Sandbox.WorkspaceRoot)
	if root == "" {
		root = fallbackRoot
		if root == "" || root == "." {
			if wd, err := os.Getwd(); err == nil {
				root = wd
			} else {
				root = "."
			}
		}
	}
	roots := []string{root}
	for _, d := range c.Sandbox.AllowWrite {
		if d = ExpandVars(d); d != "" {
			roots = append(roots, d)
		}
	}
	return roots
}

// BashMode normalises the bash-sandbox mode: only an explicit "off" disables
// it; empty or any other value resolves to "enforce", so the sandbox is on by
// default and fails safe.
func (c *Config) BashMode() string {
	if c.Sandbox.Bash == "off" {
		return "off"
	}
	return "enforce"
}

// AgentConfig configures the harness loop. PlannerModel is optional: when set
// to another provider's name it enables two-model collaboration, where the
// planner handles low-frequency planning in its own session (kept separate so
// each model's prompt prefix stays cache-stable). SubagentModel is the optional
// default for runAs=subagent skills; SubagentModels overrides it per skill name.
type AgentConfig struct {
	SystemPrompt     string            `toml:"system_prompt"`
	SystemPromptFile string            `toml:"system_prompt_file"`
	MaxSteps         int               `toml:"max_steps"`         // tool-call rounds per turn; 0 = unlimited
	PlannerMaxSteps  int               `toml:"planner_max_steps"` // planner read-only tool-call rounds; 0 = unlimited
	Temperature      float64           `toml:"temperature"`
	PlannerModel     string            `toml:"planner_model"`
	SubagentModel    string            `toml:"subagent_model"`
	SubagentModels   map[string]string `toml:"subagent_models"`
	SubagentEffort   string            `toml:"subagent_effort"`
	SubagentEfforts  map[string]string `toml:"subagent_efforts"`
	// OutputStyle selects a persona/tone block folded into the system prompt at
	// startup (a built-in like "explanatory"/"learning"/"concise", or a custom
	// .orca/output-styles/<name>.md). Empty = the unmodified prompt.
	OutputStyle string `toml:"output_style"`
	// AutoPlan controls whether interactive turns that look multi-step start in
	// plan mode automatically: "off" keeps plan mode manual, "on" enables the
	// approval gate. Legacy "ask" is treated as "on".
	AutoPlan string `toml:"auto_plan"`
	// AutoPlanClassifier optionally names a provider/model used to classify
	// borderline auto-plan decisions. Empty keeps the zero-cost heuristic path.
	AutoPlanClassifier string `toml:"auto_plan_classifier"`
	// Compaction window fractions: soft = notice only, compact = trigger, force = hard ceiling.
	SoftCompactRatio  float64 `toml:"soft_compact_ratio"`
	CompactRatio      float64 `toml:"compact_ratio"`
	CompactForceRatio float64 `toml:"compact_force_ratio"`
}

const (
	// VisionSubagentRole is the explicit per-role override used only when a
	// task requests image inspection.
	VisionSubagentRole = "vision"
	// OfficialDeepSeekVisionModel is the provider-qualified model used as the
	// built-in vision fallback when an official DeepSeek entry exposes it.
	OfficialDeepSeekVisionModel = "deepseek-v4-flash-vision-exp"
)

// ProviderEntry declares a model provider instance. ContextWindow is the model's
// token budget; the harness compacts older history as a turn's prompt approaches
// it (see agent compaction). 0 disables compaction for the instance.
type ProviderEntry struct {
	Name          string   `toml:"name"`
	Kind          string   `toml:"kind"`
	BaseURL       string   `toml:"base_url"`
	Model         string   `toml:"model"`      // a single model (back-compat)
	Models        []string `toml:"models"`     // a vendor's model list (one base_url/key, many models)
	ModelsURL     string   `toml:"models_url"` // auto-fetch models from this URL on startup
	Default       string   `toml:"default"`    // default model when Models is set (else Models[0])
	APIKeyEnv     string   `toml:"api_key_env"`
	BalanceURL    string   `toml:"balance_url"` // optional; a provider-specific wallet-balance endpoint (DeepSeek: https://api.deepseek.com/user/balance). Empty = no balance readout.
	ContextWindow int      `toml:"context_window"`
	// ModelContextWindows is an explicit per-model override. It is consulted
	// after discovered/local metadata and before the provider-wide fallback.
	ModelContextWindows map[string]int    `toml:"model_context_windows"`
	Price               *provider.Pricing `toml:"price"`
	// Thinking / Effort are provider-kind-specific knobs forwarded to the provider
	// via Config.Extra. The anthropic provider reads Thinking="adaptive" to enable
	// extended thinking and Effort ("low".."max") to tune depth. The
	// openai-compatible provider forwards Effort as reasoning_effort for
	// thinking-capable models; DeepSeek accepts high|max.
	// Empty = provider default.
	Thinking string `toml:"thinking"`
	Effort   string `toml:"effort"`
	// ReasoningProtocol selects the request shape for OpenAI-compatible reasoning
	// models. Empty/auto uses the model capability registry plus endpoint
	// heuristics; none disables automatic reasoning controls for this provider.
	ReasoningProtocol string `toml:"reasoning_protocol"`
	// SupportedEfforts lists the /effort levels this provider/model exposes.
	// When non-empty, it overrides the built-in defaults derived from
	// Kind/BaseURL and makes /effort configurable. "auto" is the implicit
	// prefix - always accepted. DefaultEffort resolves it; omit DefaultEffort
	// (or set one outside this list) to fall back to SupportedEfforts[0].
	SupportedEfforts []string `toml:"supported_efforts"`
	// DefaultEffort is the /effort level used when the user picks "auto" or
	// has not set Effort. Ignored when SupportedEfforts is empty.
	DefaultEffort string `toml:"default_effort"`
	// NoProxy reaches this provider's base_url directly, never through the proxy.
	// For China-only endpoints a foreign-exit proxy resets the TLS handshake (#2803).
	NoProxy bool `toml:"no_proxy"`

	env               map[string]string
	apiKeyOverride    string
	hasAPIKeyOverride bool
}

// ModelList returns the models this provider exposes: the explicit `models` list,
// or the single `model` as a one-element list (back-compat). Empty if neither set.
func (e *ProviderEntry) ModelList() []string {
	if len(e.Models) > 0 {
		return e.Models
	}
	if e.Model != "" {
		return []string{e.Model}
	}
	return nil
}

// IsLikelyChatModel reports whether a model ID looks like a chat/completion
// model rather than a specialised audio/vision/embedding model. It applies a
// conservative name-based heuristic - the OpenAI-compatible /models API does
// not return capability/modality metadata, so this is the most reliable
// fallback until providers add such fields.
//
// The heuristic works in two passes:
//  1. Multi-word substring check for compound terms that span separators
//     (e.g. "text-embedding", "text-to-speech").
//  2. Token-level check: the model ID is split on common separators (- _ . / :)
//     and each token is compared against a set of known non-chat keywords.
//
// "voice" is intentionally absent from the non-chat set because it is too
// broad - legitimate future chat models may include it in their name.
func IsLikelyChatModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	lower := strings.ToLower(model)

	// Pass 1: compound terms that span separator boundaries.
	var compoundNonChat = []string{
		"text-embedding", "text-to-speech", "speech-to-text",
	}
	for _, c := range compoundNonChat {
		if strings.Contains(lower, c) {
			return false
		}
	}

	// Pass 2: token-level check.
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/' || r == ':'
	})
	var nonChatTokens = map[string]bool{
		"asr": true, "stt": true, "tts": true,
		"whisper": true, "embedding": true,
		"moderation": true, "rerank": true, "dall": true,
		"transcription": true,
	}
	for _, tok := range tokens {
		if nonChatTokens[tok] {
			return false
		}
	}
	return true
}

// ChatModelList returns ModelList filtered to likely chat/completion models.
// Non-chat models (TTS, STT, ASR, embedding, etc.) are excluded so they do
// not appear in the chat model picker. Use ModelList() only when the full
// raw provider model list is needed, such as config serialization, provider
// diagnostics, or model-fetch editing.
func (e *ProviderEntry) ChatModelList() []string {
	raw := e.ModelList()
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		if IsLikelyChatModel(m) {
			out = append(out, m)
		}
	}
	return out
}

// DefaultModel returns the provider's default model: the explicit `default`, else
// the first of ModelList.
func (e *ProviderEntry) DefaultModel() string {
	if e.Default != "" {
		return e.Default
	}
	if l := e.ModelList(); len(l) > 0 {
		return l[0]
	}
	return ""
}

// HasModel reports whether m is one of the provider's models.
func (e *ProviderEntry) HasModel(m string) bool {
	for _, x := range e.ModelList() {
		if x == m {
			return true
		}
	}
	return false
}

func applyResolvedModelPricing(e *ProviderEntry) {
	if e == nil {
		return
	}
	if p := officialDeepSeekModelPricing(e, e.Model); p != nil {
		e.Price = p
	}
}

func officialDeepSeekModelPricing(e *ProviderEntry, model string) *provider.Pricing {
	if e == nil {
		return nil
	}
	return billing.OfficialDeepSeekPricing(e.Name, e.BaseURL, model)
}

// ToolsConfig selects which built-in tools are enabled. Empty means all of them.
type ToolsConfig struct {
	Enabled            []string     `toml:"enabled"`
	BashTimeoutSeconds *int         `toml:"bash_timeout_seconds"`
	Search             SearchConfig `toml:"search"`
}

// ToolLibraryConfig controls the newer host-tool groups that can be managed
// from the desktop Tool Library panel. Defaults keep the current full library.
type ToolLibraryConfig struct {
	ThreadManagementEnabled   bool `toml:"thread_management_enabled"`
	WebSearchEnabled          bool `toml:"web_search_enabled"`
	REPLRuntimeEnabled        bool `toml:"repl_runtime_enabled"`
	DocumentToolsEnabled      bool `toml:"document_tools_enabled"`
	HostSystemToolsEnabled    bool `toml:"host_system_tools_enabled"`
	ConversationSearchEnabled bool `toml:"conversation_search_enabled"`
	ProactiveToolUseEnabled   bool `toml:"proactive_tool_use_enabled"`
}

func DefaultToolLibrarySettings() ToolLibraryConfig {
	return ToolLibraryConfig{
		ThreadManagementEnabled:   true,
		WebSearchEnabled:          true,
		REPLRuntimeEnabled:        true,
		DocumentToolsEnabled:      true,
		HostSystemToolsEnabled:    true,
		ConversationSearchEnabled: true,
		ProactiveToolUseEnabled:   true,
	}
}

const defaultBashTimeoutSeconds = 120

// BashTimeoutSeconds returns the foreground bash timeout in seconds. An omitted
// config keeps the historical 120s safety cap, explicit 0 disables the
// tool-local cap, and positive values set a custom cap. Negative values fall
// back to the default so a typo cannot silently remove the safety net.
func (c *Config) BashTimeoutSeconds() int {
	if c.Tools.BashTimeoutSeconds == nil || *c.Tools.BashTimeoutSeconds < 0 {
		return defaultBashTimeoutSeconds
	}
	return *c.Tools.BashTimeoutSeconds
}

// SearchConfig tunes the grep tool's engine. Engine is "auto" (default - use
// ripgrep when it's on PATH, else the native Go scanner), "native" (always Go),
// or "rg" (require ripgrep; warn at startup and fall back to native if absent).
// RgPath optionally points at a specific ripgrep binary instead of a PATH lookup.
type SearchConfig struct {
	Engine string `toml:"engine"`
	RgPath string `toml:"rg_path"`
}

// PermissionsConfig declares the per-call permission policy (see
// internal/permission). Mode is the fallback decision for writer tools when no
// rule matches ("ask" | "allow" | "deny"; default "ask"); read-only tools always
// fall back to allow. Allow/Ask/Deny are rule lists of the form "ToolName" or
// "ToolName(glob)". Precedence: deny > ask > allow > fallback.
type PermissionsConfig struct {
	Mode            string   `toml:"mode"`
	AutoReviewModel string   `toml:"auto_review_model"`
	Allow           []string `toml:"allow"`
	Ask             []string `toml:"ask"`
	Deny            []string `toml:"deny"`
}

// PluginEntry declares an external MCP server. Type selects the transport:
// "stdio" (default) launches Command/Args/Env as a subprocess; "http"
// (a.k.a. streamable-http) connects to a remote URL with optional static
// Headers. Legacy "sse" entries remain readable for startup compatibility but
// are rejected by save/import validation. String fields support ${VAR} /
// ${VAR:-default} expansion so
// secrets (bearer tokens, keys) come from the environment, not the file. The
// fields mirror Claude Code's mcpServers spec, so entries can come from either
// deepseek-orca.toml's [[plugins]] or a project-root .mcp.json (see loadMCPJSON).
type PluginEntry struct {
	Name    string            `toml:"name"`
	Type    string            `toml:"type"` // "stdio" (default) | "http"; legacy "sse" is read-only
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	URL     string            `toml:"url"`
	Headers map[string]string `toml:"headers"`
	// AutoStart controls whether the server connects during session startup.
	// Nil preserves historical behavior: configured servers start automatically.
	AutoStart *bool `toml:"auto_start"`
	// Tier selects how aggressively the server is connected at boot:
	//   "eager"      - blocks startup until the handshake completes; required for
	//                  servers whose tools the system prompt depends on.
	//   "lazy"       - registers placeholder tools immediately (from on-disk
	//                  schema cache when available) and only spawns the real
	//                  subprocess on first model use. Kept for legacy configs.
	//   "background" - placeholder + spawn fired at boot but not waited on;
	//                  swap happens once the spawn finishes.
	// Empty defaults to "background" so enabled MCPs connect automatically
	// without blocking chat. Unknown non-empty values fall back to "lazy".
	Tier string `toml:"tier"`

	// env is the loaded Config's private workspace scope. It is intentionally
	// unexported so MCP dotenv values never enter TOML or JSON serialization.
	env map[string]string
}

func (e PluginEntry) ShouldAutoStart() bool {
	return e.AutoStart == nil || *e.AutoStart
}

// ResolvedTier returns the normalized tier ("eager"|"lazy"|"background") with
// the project default applied. Unknown values fall back to "lazy" so a typo
// never forces a slow boot.
func (e PluginEntry) ResolvedTier() string {
	return resolvedMCPTier(e.Tier)
}

func resolvedMCPTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background":
		return "background"
	case "":
		return "background"
	default:
		return "lazy"
	}
}

func (c *Config) AutoStartPlugins() []PluginEntry {
	out := make([]PluginEntry, 0, len(c.Plugins))
	for _, p := range c.Plugins {
		if p.ShouldAutoStart() {
			out = append(out, p)
		}
	}
	return out
}

func boolPtr(v bool) *bool { return &v }

// DefaultSystemPrompt is used when config provides none.
const DefaultSystemPrompt = `你是 Orca，一个专注于执行代码任务的智能编程 Agent。
你可以使用系统提供的工具读取和写入文件、运行 shell 命令，并在需要时检索项目上下文。
工作原则：先理解用户请求再行动；用工具验证事实，不要凭空猜测；保持修改范围小、正确且符合项目既有风格；完成后简要说明做了什么以及如何验证。
当请求中存在需要用户真正决策的选择，例如实现方案、库选型、工作范围或会产生明显后果的歧义时，使用 ask 工具给出 2 到 4 个具体选项，而不是自行猜测或把问题埋在回复里。若存在明显默认选择，则直接采用；不要为了形式确认而提问。权限绕过模式不能替用户回答 ask 问题，也不能替用户批准计划。若没有可交互的用户，ask 工具会返回模型假设的兜底结果；继续前请说明你采用了什么假设。
对于多步骤工作，使用 todo_write 跟踪进度：列出步骤，始终只保留一个 in_progress，并在完成每一步时立即更新为 completed。进度清单要随工作推进实时更新，而不是只在最后一次性更新。
在 Plan 模式下，宿主会阻止写入类工具：你只能做只读研究，然后以回复形式给出简洁计划并停止。用户批准前不要修改任何内容；批准后按步骤执行，并持续更新任务列表。
在提到宿主应用时，请称呼它为 Orca。不要在面向用户的回复或生成的文档中使用旧产品名，除非用户正在讨论从旧名称迁移。`

const DefaultAgentSystemPrompt = `# SYSTEM INSTRUCTIONS

You are Orca, a coding agent. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled.

# General

You bring a senior engineer's judgment to the work. Read the codebase first, prefer existing project patterns, keep changes scoped to the request, and verify with useful tests or checks when feasible.

When the user asks for implementation, do not stop at a proposal unless they explicitly asked for a plan or discussion only. Carry the work through the change, verification, and a concise final summary.

When a decision truly belongs to the user, use ask or ask a concise question. If a reasonable default exists, proceed with it and state the assumption briefly.

When referring to the host application, call it Orca. Do not use legacy product names unless the user is explicitly discussing migration from those names.`

// TaskTrackingPolicy is appended to normal and enhanced prompt profiles. Keep it
// shared so both profiles teach the same automatic Todo behavior.
const TaskTrackingPolicy = `Task tracking policy:
- For complex, multi-stage, cross-file, debugging, build/release, migration, or long-running tasks, use todo_write before implementation to create a concise task list.
- For simple answers, quick checks, or small single-step edits, do not create a todo list just for ceremony.
- When using todo_write, send the complete list every time; keep exactly one item in_progress; update an item as soon as it is completed.`

// ToolRoutingPolicy is appended to normal and enhanced prompt profiles. Keep it
// stable and concise: it is part of the provider-visible prompt prefix.
const ToolRoutingPolicy = `工具选择规则：
- 文件和代码：优先使用 read_file、grep、ls、glob、edit_file、write_file、multi_edit；不要用 shell cat/grep/ls/sed 代替这些专用工具。
- 开发命令：bash 主要用于构建、测试、git、包管理器和普通项目 shell 命令。
- 系统/宿主操作：涉及操作系统状态、进程、应用启动、剪贴板、通知、定时自动化、Windows 原生命令、联网搜索、持久 REPL 或文档提取时，先考虑对应 host 工具。
- host_command 是原生宿主命令兜底；Windows 上它使用 cmd/powershell 语义，通常比 bash 更适合 Windows 原生命令。
- 自动化：只有用户明确要求重复性、持续性或后台监控类任务时才用 automation_create，并提供清晰 label；用 automation_list/automation_cancel 管理状态。
- 运行时：计算、JSON/数据转换、临时脚本和可复用变量优先用 node_repl_exec 或 python_repl_exec，避免反复拼复杂 shell one-liner。
- 文档：Word、PowerPoint、Excel、PDF 先用 document_inspect 和 document_extract；复杂处理再配合 python_repl_exec。
- 联网：不知道 URL 时用 web_search；已有具体 URL 时再用 web_fetch。
- 工具失败后先阅读结构化 status/error，再修正参数、换推荐的兜底工具或解释阻塞原因；不要原样重复同一个失败调用。`

// LanguagePolicy is the auto fallback appended to the system prompt when no
// concrete UI language is resolved. It is static English text, so it stays part
// of the cache-stable prefix and avoids per-turn language injection.
const LanguagePolicy = `请使用用户最新消息所使用的语言回复：用户用中文就用中文，用户用英文就用英文，并在用户切换语言时同步切换。` +
	`这也应影响你的思考和表述方式。代码、标识符、文件路径、shell 命令以及必须保持原样的技术术语不要翻译。`

// ActiveToolRoutingPolicy is the default clean provider-visible tool policy used
// by current prompt builders. It intentionally stays in English to avoid
// corrupting model-visible tool context.
var ActiveToolRoutingPolicy = BuildActiveToolRoutingPolicy(DefaultToolLibrarySettings())

func BuildVisionPolicy(enabled bool) string {
	if enabled {
		return "Native image input is enabled. When the user message includes attached images, inspect the image content directly and use visual evidence in the response. Do not claim that you cannot see an image that is present in the message."
	}
	return "Native image input is disabled. Image references provide paths and metadata only; do not claim to have inspected their pixels. Use an available OCR or vision tool when visual understanding is required."
}

func BuildActiveToolRoutingPolicy(settings ToolLibraryConfig) string {
	settings = NormalizeToolLibrarySettings(settings)
	lines := []string{"Tool use and evidence policy:"}
	if settings.ProactiveToolUseEnabled {
		lines = append(lines, "- Be evidence-first: when an answer depends on current facts, external information, repository state, file contents, command output, runtime behavior, or the user's local environment, use the appropriate tool before answering instead of guessing from memory.")
	}
	lines = append(lines,
		"- For files and code, prefer read_file, grep, ls, glob, edit_file, write_file, and multi_edit. Do not substitute shell cat/grep/ls/sed when a dedicated tool fits.",
		"- For development commands, use bash mainly for builds, tests, git, package managers, and ordinary project shell tasks.",
	)
	if settings.HostSystemToolsEnabled || settings.ThreadManagementEnabled || settings.WebSearchEnabled || settings.REPLRuntimeEnabled || settings.DocumentToolsEnabled || settings.ConversationSearchEnabled {
		lines = append(lines, "- For managed host capabilities, prefer the matching dedicated host tool before falling back to shell.")
	}
	if settings.HostSystemToolsEnabled {
		lines = append(lines,
			"- Use host_command as the native OS command fallback; on Windows it uses cmd/powershell semantics and is usually better than bash for Windows-native commands.",
			"- Use host_system_info, host_list_processes, host_kill_process, host_open_app, host_clipboard, and notify_user for system state, processes, app launch, clipboard, and direct notifications.",
			"- Use automation_create only when the user clearly asks for recurring, continuous, or background-monitoring work. Use automation_list and automation_cancel to inspect or manage existing automations.",
		)
	}
	if settings.ThreadManagementEnabled {
		lines = append(lines, "- Use thread_list when you need to inspect saved Orca conversation threads/topics.")
	}
	if settings.ConversationSearchEnabled {
		lines = append(lines, "- Use conversation_search to find older user/model conversation details after context compression; use conversation_read with a returned locator when you need the fuller nearby transcript.")
	}
	if settings.REPLRuntimeEnabled {
		lines = append(lines, "- Use node_repl_exec or python_repl_exec for calculations, JSON/data transformations, quick scripts, reusable variables, and checks where a persistent runtime is clearer than a complex shell one-liner.")
	}
	if settings.DocumentToolsEnabled {
		lines = append(lines, "- Use document_inspect and document_extract for Word, PowerPoint, Excel, PDF, and similar document work; combine with python_repl_exec for complex processing when the REPL runtime is enabled.")
	}
	if settings.WebSearchEnabled {
		lines = append(lines, "- Use web_search when you do not know the URL and need current web information; use web_fetch when you already have a specific URL or after selecting a search result.")
	} else {
		lines = append(lines, "- Use web_fetch only when you already have a specific URL.")
	}
	lines = append(lines, "- If a tool fails, read the structured status/error, fix the parameters, choose the recommended fallback tool, or explain the blocker. Do not repeat the same failing call unchanged.")
	return strings.Join(lines, "\n")
}

// BuildWorkToolRoutingPolicy describes the non-coding Work surface without
// naming coding-only shell and symbol tools that are absent from Assistant and
// Orca registries.
func BuildWorkToolRoutingPolicy(settings ToolLibraryConfig) string {
	settings = NormalizeToolLibrarySettings(settings)
	lines := []string{
		"Work tool use and evidence policy:",
		"- Use the available file and artifact tools to inspect inputs, create deliverables, and validate every written artifact before reporting completion.",
		"- Prefer a dedicated host, document, web, automation, or REPL tool over a generic command when both can perform the task.",
	}
	if settings.HostSystemToolsEnabled {
		lines = append(lines,
			"- Use host_command for native OS commands; use host_system_info, host_list_processes, host_kill_process, host_open_app, host_clipboard, and notify_user for their matching host actions.",
			"- Use automation_create only for genuinely recurring or future work; use automation_list and automation_cancel to manage it.",
		)
	}
	if settings.ThreadManagementEnabled {
		lines = append(lines, "- Use thread_list only when saved conversation topics are relevant.")
	}
	if settings.ConversationSearchEnabled {
		lines = append(lines, "- Use conversation_search and conversation_read to recover older local transcript details only when the current request needs them.")
	}
	if settings.REPLRuntimeEnabled {
		lines = append(lines, "- Use node_repl_exec or python_repl_exec for calculations, data transformations, and reusable runtime work.")
	}
	if settings.DocumentToolsEnabled {
		lines = append(lines, "- Use document_inspect and document_extract to inspect Office and PDF inputs; use artifact_create, artifact_edit, artifact_preview, and artifact_validate for structured outputs.")
	} else {
		lines = append(lines, "- Use artifact_create, artifact_edit, artifact_preview, and artifact_validate for structured DOCX, XLSX, PPTX, and PDF outputs.")
	}
	if settings.WebSearchEnabled {
		lines = append(lines, "- Use web_search for current information when the URL is unknown and web_fetch for a known URL.")
	} else {
		lines = append(lines, "- Use web_fetch only for a URL already provided or known.")
	}
	lines = append(lines, "- If a tool fails, correct the request, use a real available fallback, or report the blocker; never fabricate a result.")
	return strings.Join(lines, "\n")
}

func BuildBashHostToolSteer(settings ToolLibraryConfig) string {
	settings = NormalizeToolLibrarySettings(settings)
	parts := []string{}
	if settings.HostSystemToolsEnabled {
		parts = append(parts, "host_command for native OS commands", "host_list_processes/host_kill_process for processes", "host_open_app for launching apps", "host_clipboard for clipboard", "notify_user for direct notifications")
	}
	parts = append(parts, "automation_create only for clearly recurring/continuous/background-monitoring tasks")
	if settings.ThreadManagementEnabled {
		parts = append(parts, "thread_list for saved conversation topics")
	}
	if settings.ConversationSearchEnabled {
		parts = append(parts, "conversation_search/conversation_read for older local transcript details")
	}
	if settings.WebSearchEnabled {
		parts = append(parts, "web_search for unknown URLs")
	}
	if settings.REPLRuntimeEnabled {
		parts = append(parts, "node_repl_exec/python_repl_exec for reusable runtime work")
	}
	if settings.DocumentToolsEnabled {
		parts = append(parts, "document_inspect/document_extract for Word/PPT/Excel/PDF files")
	}
	if len(parts) == 0 {
		return ""
	}
	return " For host/system actions, prefer enabled dedicated host tools before bash: " + strings.Join(parts, ", ") + "."
}

func NormalizeToolLibrarySettings(settings ToolLibraryConfig) ToolLibraryConfig {
	return settings
}

// ActiveLanguagePolicy is the clean provider-visible language policy used by
// current prompt builders.
const ActiveLanguagePolicy = `Reply in the language used by the user's latest message. If the user writes Chinese, reply in Chinese; if the user writes English, reply in English. Preserve code, identifiers, file paths, shell commands, and required technical terms exactly.`

// Default returns the provider-neutral built-in configuration. Official cloud
// presets live in the provider catalog and are materialised only when selected.
func Default() *Config {
	return &Config{
		ConfigVersion: 11,
		// DeepSeek remains a usable built-in default for existing and unattended
		// CLI configurations, but first launch no longer requires its key: the
		// desktop onboarding overlay can replace this choice before sending.
		DefaultModel: "deepseek-flash",
		UI:           UIConfig{Theme: "auto"},
		Desktop:      DesktopConfig{Language: "zh", Theme: "light", ThemeStyle: "slate", UIStyle: DesktopUIStyleModern, CheckUpdates: boolPtr(true), VisionMode: VisionModeAuto, ActivityIndicator: true, ConversationMode: "assistant"},
		LocalAI:      LocalAIConfig{IdleUnloadMinutes: 10, VRAMReserveMiB: 2048},
		Notifications: NotificationsConfig{
			Enabled:         false,
			TurnDone:        true,
			ApprovalRequest: true,
			AskRequest:      true,
		},
		Agent: AgentConfig{
			SystemPrompt: DefaultAgentSystemPrompt,
			// 0 = no step cap: the agent loops until the model gives a final answer,
			// the user cancels, or the provider errors. Context stays bounded by
			// compaction, not by a round count. Set a positive agent.max_steps only
			// if you want a hard guard against runaway.
			MaxSteps:          0,
			PlannerMaxSteps:   12,
			AutoPlan:          "off",
			SoftCompactRatio:  0.5,
			CompactRatio:      0.8,
			CompactForceRatio: 0.9,
		},
		// Mode "ask" with no rules keeps `deepseek-orca run` autonomous (no TTY -> ask
		// resolves to allow) while `deepseek-orca chat` prompts before writers. Users add
		// deny/allow rules to harden or quiet specific tools.
		Permissions: PermissionsConfig{Mode: "ask"},
		ToolLibrary: DefaultToolLibrarySettings(),
		// Sandbox on by default: bash is jailed (macOS), network allowed so
		// builds/downloads work. Set bash = "off" to disable. Network=true here
		// so an absent [sandbox] in a user's file keeps egress (zero value would
		// wrongly deny it).
		Sandbox: SandboxConfig{Bash: "enforce", Network: true},
		// CodeGraph code-intelligence defaults on so existing configs (which never
		// wrote a [codegraph] section) keep it after an upgrade. First-run scaffolds
		// write enabled = false instead, so only brand-new users start without it.
		// AutoInstall fetches the runtime into the cache when enabled and missing.
		Codegraph: CodegraphConfig{Enabled: true, AutoInstall: true},
		// LSP tools on by default, but dormant until a language server is on PATH;
		// a missing server yields an install hint rather than an error.
		LSP:     LSPConfig{Enabled: true},
		Network: NetworkConfig{ProxyMode: netclient.ModeAuto},
		Bot: BotConfig{
			Enabled:    true,
			MaxSteps:   25,
			DebounceMs: 1500,
			Allowlist:  BotAllowlist{Enabled: true, AllowAll: true},
			QQ:         QQBotConfig{AppSecretEnv: "QQ_BOT_APP_SECRET", Environment: "production"},
			Feishu:     FeishuBotConfig{Domain: "feishu", AppSecretEnv: "FEISHU_BOT_APP_SECRET", Mode: "webhook", WebhookPort: 8080, RequireMention: true},
			Weixin:     WeixinBotConfig{AccountID: "default", TokenEnv: "WEIXIN_BOT_TOKEN", APIBase: "https://ilinkai.weixin.qq.com"},
		},
		Providers: []ProviderEntry{
			{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance", ContextWindow: 1_000_000, Price: officialDeepSeekModelPricing(&ProviderEntry{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com"}, "deepseek-v4-flash")},
			{Name: "deepseek-pro", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance", ContextWindow: 1_000_000, Price: officialDeepSeekModelPricing(&ProviderEntry{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com"}, "deepseek-v4-pro")},
		},
	}
}

// Load builds the configuration: defaults, then user config, then project
// config, then MCP servers from Claude Code's .mcp.json, then (lowest priority)
// the v0.x ~/.orca/config.json's mcpServers. A .env in the working directory
// is loaded first so api_key_env can resolve.
func Load() (*Config, error) {
	return LoadForRoot(".")
}

// LoadForRoot builds the configuration with project files resolved from root
// instead of the current working directory. When root is "" or ".", it behaves
// like Load(). This is the workspace-aware entry point: desktop tabs use it so
// each project's deepseek-orca.toml + .env + .mcp.json are resolved independently
// without changing the process cwd.
func LoadForRoot(root string) (*Config, error) {
	root = resolveRoot(root)
	if err := EnsureV11StateMigration(); err != nil {
		slog.Warn("config: V11 state migration failed; continuing with compatibility reads", "err", err)
	}
	projectEnv := loadDotEnvForRoot(root)
	cfg := Default()
	cfg.env = projectEnv

	projectTOML := product.ProjectConfigName
	if root != "." {
		projectTOML = filepath.Join(root, product.ProjectConfigName)
	}

	var tomlSources []string
	if legacy := legacyUserConfigPath(); legacy != "" {
		tomlSources = append(tomlSources, legacy)
	}
	if uc := userConfigPath(); uc != "" {
		tomlSources = append(tomlSources, uc)
	}
	legacyProjectTOML := product.LegacyProjectConfigName
	if root != "." {
		legacyProjectTOML = filepath.Join(root, product.LegacyProjectConfigName)
	}
	// Legacy files are read first; V3 names win without mutating tracked files.
	tomlSources = append(tomlSources, legacyProjectTOML, projectTOML)
	sawConfigFile := false
	for _, path := range tomlSources {
		if _, err := os.Stat(path); err == nil {
			sawConfigFile = true
			if err := migrateLegacyMCPTiersFile(path); err != nil {
				slog.Warn("config: legacy mcp tier migration failed", "path", path, "err", err)
			}
		}
		if err := mergeFile(cfg, path); err != nil {
			return nil, err
		}
	}
	// toml.DecodeFile replaces [[plugins]] wholesale, so cfg.Plugins now holds
	// only the last file's. Re-merge by name across all sources (later wins) so a
	// project deepseek-orca.toml doesn't drop the global config's MCP servers.
	plugins, err := mergeTOMLPlugins(tomlSources)
	if err != nil {
		return nil, err
	}
	cfg.Plugins = plugins
	providers, err := mergeTOMLProviders(tomlSources, cfg.Providers)
	if err != nil {
		return nil, err
	}
	cfg.Providers = providers

	// Claude Code's .mcp.json (project root) is read last and merged into
	// [[plugins]], so a server configured for Claude works here unchanged.
	// deepseek-orca.toml wins on a name collision (see mergeMCPJSON).
	mcpFile := mcpJSONFile
	if root != "." {
		mcpFile = filepath.Join(root, mcpJSONFile)
	}
	entries, err := loadMCPJSON(mcpFile)
	if err != nil {
		return nil, err
	}
	cfg.mergeMCPJSON(entries)

	// Lowest priority: the v0.x ~/.orca/config.json's mcpServers, so upgrading
	// from the TypeScript line keeps MCP servers without rewriting them. Anything
	// the v2 config or .mcp.json already declared wins on a name collision.
	cfg.mergeMCPJSON(loadLegacyMCP(legacyConfigPath()))
	bindProviderEnv(cfg)
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeDesktopUpdatePreference(cfg)
	normalizeDesktopVisionPreference(cfg)
	normalizeAutomationPreference(cfg)
	normalizeDesktopUIScale(cfg)
	normalizeAutomationFullAccess(cfg)
	normalizeActivityIndicatorPreference(cfg)
	normalizeConversationProfiles(cfg)
	normalizeV11ProductSettings(cfg)
	normalizeEffortConfig(cfg)
	backfillDeepSeekPro(cfg)
	bindProviderEnv(cfg)
	// First run (no config file anywhere): keep CodeGraph off until the user opts
	// in. An existing config - even one without a [codegraph] section - keeps the
	// built-in default (on), so an upgrade never silently drops code intelligence.
	if !sawConfigFile {
		cfg.Codegraph.Enabled = false
	}
	return cfg, nil
}

// backfillDeepSeekPro restores deepseek-pro for configs the pre-fix setup wizard
// wrote with only deepseek-v4-flash: a keyless /models probe used to drop the Pro
// SKU, leaving users unable to switch to it. In-memory only - the user's file is
// untouched. Narrowly scoped to the official DeepSeek endpoint (which is known to
// serve pro) so a custom flash-only deployment isn't given an entry that 404s.
func backfillDeepSeekPro(c *Config) {
	const flashModel, proModel = "deepseek-v4-flash", "deepseek-v4-pro"
	var flash *ProviderEntry
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == "deepseek-pro" {
			return
		}
		for _, m := range p.ModelList() {
			switch m {
			case proModel:
				return // pro already reachable
			case flashModel:
				if strings.Contains(p.BaseURL, "api.deepseek.com") {
					flash = p
				}
			}
		}
	}
	if flash == nil {
		return
	}
	for _, bp := range Default().Providers {
		if bp.Name == "deepseek-pro" {
			bp.APIKeyEnv = flash.APIKeyEnv
			c.Providers = append(c.Providers, bp)
			return
		}
	}
}

func resolveRoot(root string) string {
	if root == "" || root == "." {
		return "."
	}
	return filepath.Clean(root)
}

// normalizeLegacyEffort migrates the retired DeepSeek effort="off" (the old
// /thinking off that disabled thinking) to the provider default, so a config
// written by an older version keeps loading instead of erroring on a value the
// provider no longer accepts.
func normalizeLegacyEffort(c *Config) {
	for i := range c.Providers {
		if strings.EqualFold(strings.TrimSpace(c.Providers[i].Effort), "off") {
			c.Providers[i].Effort = ""
		}
	}
}

// mergeTOMLPlugins merges [[plugins]] across TOML sources by name (later source wins).
func mergeTOMLPlugins(paths []string) ([]PluginEntry, error) {
	var merged []PluginEntry
	index := map[string]int{}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		if _, err := toml.DecodeFile(path, &f); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
		for _, p := range f.Plugins {
			p, _ = NormalizePluginCommandLine(p)
			if i, ok := index[p.Name]; ok {
				merged[i] = p
				continue
			}
			index[p.Name] = len(merged)
			merged = append(merged, p)
		}
	}
	return merged, nil
}

// mergeTOMLProviders merges provider entries by name across the user and
// project sources. TOML array decoding replaces the whole slice, which made a
// project-local provider block hide unrelated account-level providers. A later
// source still wins for the same provider name, preserving explicit project
// overrides without dropping the rest of the catalog.
func mergeTOMLProviders(paths []string, fallback []ProviderEntry) ([]ProviderEntry, error) {
	merged := append([]ProviderEntry(nil), fallback...)
	index := make(map[string]int, len(merged))
	seeded := false
	for i, p := range merged {
		if strings.TrimSpace(p.Name) != "" {
			index[p.Name] = i
		}
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		if _, err := toml.DecodeFile(path, &f); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
		if len(f.Providers) == 0 {
			continue
		}
		// A source with providers is authoritative for the default catalog: start
		// with the first such source, then merge later sources by name.
		if !seeded {
			merged = nil
			index = map[string]int{}
			seeded = true
		}
		for _, p := range f.Providers {
			if i, ok := index[p.Name]; ok {
				merged[i] = p
				continue
			}
			index[p.Name] = len(merged)
			merged = append(merged, p)
		}
	}
	return merged, nil
}

// LoadForEdit returns a config to seed the `deepseek-orca setup` wizard when reconfiguring:
// the built-in defaults with the file at path (if present) decoded on top, so a
// reconfigure preserves the user's existing providers and agent settings instead
// of resetting to defaults. .env is loaded so api_key_env resolution works while
// the wizard decides which keys are still missing.
func LoadForEdit(path string) *Config {
	cfgEnv := loadDotEnvForRoot(filepath.Dir(path))
	cfg := Default()
	cfg.env = cfgEnv
	if _, err := os.Stat(path); err == nil {
		if err := migrateLegacyMCPTiersFile(path); err != nil {
			slog.Warn("config: legacy mcp tier migration failed", "path", path, "err", err)
		}
	}
	if err := mergeFile(cfg, path); err != nil {
		slog.Warn("config: load for edit failed, using defaults", "path", path, "err", err)
	}
	bindProviderEnv(cfg)
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeDesktopUpdatePreference(cfg)
	normalizeDesktopVisionPreference(cfg)
	normalizeAutomationPreference(cfg)
	normalizeDesktopUIScale(cfg)
	normalizeAutomationFullAccess(cfg)
	normalizeActivityIndicatorPreference(cfg)
	normalizeConversationProfiles(cfg)
	normalizeV11ProductSettings(cfg)
	normalizeEffortConfig(cfg)
	bindProviderEnv(cfg)
	return cfg
}

func bindProviderEnv(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		c.Providers[i].env = c.env
	}
	for i := range c.Plugins {
		c.Plugins[i].env = c.env
	}
}

// V2 shipped check_updates=false without exposing a setting. V3 enables the
// feature once, then preserves an explicit V3 opt-out.
func normalizeDesktopUpdatePreference(c *Config) {
	if c == nil || c.ConfigVersion >= 3 {
		return
	}
	enabled := true
	c.Desktop.CheckUpdates = &enabled
	c.ConfigVersion = 3
}

func BuildVisionModePolicy(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case VisionModeOn:
		return BuildVisionPolicy(true)
	case VisionModeAuto:
		return `<vision-policy>
Vision mode is automatic. Image bytes are sent only to models whose visual capability has been confirmed. If the current model receives only an image snapshot reference, do not claim to have seen it. Use the task tool with its images field and a confirmed vision-capable model only when visual inspection is needed.
</vision-policy>`
	default:
		return BuildVisionPolicy(false)
	}
}

// V4 replaces the old boolean vision flag with an explicit off|auto|on mode.
// Existing users keep their exact behavior; only fresh V4 installs default to auto.
func normalizeDesktopVisionPreference(c *Config) {
	if c == nil {
		return
	}
	if c.ConfigVersion < 4 {
		if c.Desktop.VisionEnabled {
			c.Desktop.VisionMode = VisionModeOn
		} else {
			c.Desktop.VisionMode = VisionModeOff
		}
		c.ConfigVersion = 4
	}
	mode := c.DesktopVisionMode()
	c.Desktop.VisionMode = mode
	c.Desktop.VisionEnabled = mode == VisionModeOn
	if c.ConfigVersion < 5 {
		c.ConfigVersion = 5
	}
}

// V5 introduces the automation workspace and shared assistant profile. The
// feature has no destructive config migration; the marker lets future versions
// distinguish configurations that have passed through the new defaults.
func normalizeAutomationPreference(c *Config) {
	if c != nil && c.ConfigVersion < 5 {
		c.ConfigVersion = 5
	}
}

// V3 ignores the retired application-level zoom and relies on Per-Monitor DPI.
func normalizeDesktopUIScale(c *Config) {
	if c == nil {
		return
	}
	c.Desktop.UIScale = 0
	if c.ConfigVersion < 6 {
		c.ConfigVersion = 6
	}
}

// V7 records the explicit one-time consent required before automation turns
// may bypass ordinary tool and plan approvals. Existing installations remain
// unapproved until the user confirms in the desktop UI.
func normalizeAutomationFullAccess(c *Config) {
	if c != nil && c.ConfigVersion < 7 {
		c.ConfigVersion = 7
	}
}

// V8 turns the independent activity indicator on once for upgraded installs.
// From V8 onward an explicit opt-out is persisted and left untouched.
func normalizeActivityIndicatorPreference(c *Config) {
	if c != nil && c.ConfigVersion < 8 {
		c.Desktop.ActivityIndicator = true
		c.ConfigVersion = 8
	}
}

// V9 replaces normal/enhanced with coding and reserves Orca for the fixed
// automation conversation. conversation_mode only controls ordinary chats.
func normalizeConversationProfiles(c *Config) {
	if c == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(c.Desktop.ConversationMode)) {
	case "coding":
		c.Desktop.ConversationMode = "coding"
	default:
		c.Desktop.ConversationMode = "assistant"
	}
	c.Bot.PromptMode = "orca"
	if c.ConfigVersion < 9 {
		c.ConfigVersion = 9
	}
	if c.ConfigVersion < 10 {
		c.ConfigVersion = 10
	}
}

// V11 introduces provider-neutral onboarding and optional local/Computer Use
// roles. Existing installations have already completed setup, so they must not
// be forced through the new first-run flow after upgrading.
func normalizeV11ProductSettings(c *Config) {
	if c == nil {
		return
	}
	if c.ConfigVersion < 11 {
		c.Desktop.OnboardingCompleted = true
		if c.LocalAI.IdleUnloadMinutes == 0 {
			c.LocalAI.IdleUnloadMinutes = 10
		}
		if c.LocalAI.VRAMReserveMiB == 0 {
			c.LocalAI.VRAMReserveMiB = 2048
		}
		c.ConfigVersion = 11
	}
}

// mergeFile decodes a TOML file onto cfg if it exists. An absent file is not an error.
func mergeFile(cfg *Config, path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}

// normalizeLegacyMCPTiers keeps loaded legacy config files on the new product
// behavior: enabled MCP servers connect in the background by default, and the
// retired per-server startup tier is no longer a user-facing setting.
func normalizeLegacyMCPTiers(c *Config) {
	if c == nil {
		return
	}
	c.Codegraph.Tier = ""
	for i := range c.Plugins {
		c.Plugins[i].Tier = ""
	}
}

func migrateLegacyMCPTiersFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, changed := stripLegacyMCPTierLines(string(raw))
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(next), info.Mode().Perm())
}

func stripLegacyMCPTierLines(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	section := ""
	changed := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if header := tomlSectionHeader(line); header != "" {
			section = header
		}
		if (section == "codegraph" || section == "plugins") && isTOMLKeyAssignment(line, "tier") {
			changed = true
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), changed
}

func tomlSectionHeader(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return ""
	}
	if i := strings.Index(trimmed, "#"); i >= 0 {
		trimmed = strings.TrimSpace(trimmed[:i])
	}
	switch trimmed {
	case "[codegraph]":
		return "codegraph"
	case "[[plugins]]":
		return "plugins"
	default:
		return "other"
	}
}

func isTOMLKeyAssignment(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, key) {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
	return strings.HasPrefix(rest, "=")
}

// normalizeLegacyProviderModels repairs provider entries written by older
// desktop builds that carried the official provider name/endpoint but omitted the
// model field. The repair is intentionally narrow: valid user-provided model
// lists are left untouched, while known official aliases get the model implied by
// their preset name so model pickers and provider validation have an option.
func normalizeLegacyProviderModels(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if providerHasAnyModel(*p) {
			continue
		}
		if model := legacyOfficialProviderModel(p.Name); model != "" {
			p.Model = model
		}
	}
}

func legacyOfficialProviderModel(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash":
		return "deepseek-v4-flash"
	case "deepseek-pro":
		return "deepseek-v4-pro"
	case "mimo-api", "mimo-pro":
		return "mimo-v2.5-pro"
	case "mimo-flash":
		return "mimo-v2.5"
	default:
		return ""
	}
}

func normalizeDesktopOfficialProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) == 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	next := make([]string, 0, len(c.Desktop.ProviderAccess))
	includeMimoFlash := false
	for _, name := range c.Desktop.ProviderAccess {
		if strings.TrimSpace(name) == "mimo-flash" {
			includeMimoFlash = true
		}
		name = canonicalDesktopOfficialProviderName(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	c.Desktop.ProviderAccess = next
	if seen["deepseek"] {
		ensureDeepSeekOfficialProvider(c)
	}
	if seen["mimo-api"] {
		ensureMimoAPIProvider(c)
	}
	if seen["mimo-token-plan"] {
		ensureMimoTokenPlanProvider(c, includeMimoFlash)
	}
	retargetDesktopOfficialRefs(c, seen)
}

// NormalizeLegacyDesktopProviderAccess seeds the desktop provider-access list
// for configs written before Settings tracked explicit provider access. Callers
// should only use this when they know the TOML did not declare provider_access;
// an explicit empty list means the user removed all access entries.
func NormalizeLegacyDesktopProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) > 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	var access []string
	add := func(name string) {
		name = canonicalDesktopOfficialProviderName(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		access = append(access, name)
	}
	addRef := func(ref string) {
		if entry, ok := c.ResolveModel(ref); ok {
			if !entry.Configured() {
				return
			}
			add(entry.Name)
		}
	}
	addRef(c.DefaultModel)
	addRef(c.Agent.PlannerModel)
	addRef(c.Agent.SubagentModel)
	addRef(c.Agent.AutoPlanClassifier)
	for _, ref := range c.Agent.SubagentModels {
		addRef(ref)
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Configured() {
			add(p.Name)
		}
	}
	if len(access) == 0 {
		return
	}
	c.Desktop.ProviderAccess = access
	normalizeDesktopOfficialProviderAccess(c)
}

func canonicalDesktopOfficialProviderName(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash", "deepseek-pro":
		return "deepseek"
	case "mimo", "xiaomi-mimo", "xiaomi_mimo":
		return "mimo-api"
	case "mimo-pro", "mimo-flash":
		return "mimo-token-plan"
	default:
		return strings.TrimSpace(name)
	}
}

// CanonicalDesktopOfficialProviderName returns the Settings Center provider ID
// for built-in official provider aliases.
func CanonicalDesktopOfficialProviderName(name string) string {
	return canonicalDesktopOfficialProviderName(name)
}

func desktopProviderAccessMap(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		name = canonicalDesktopOfficialProviderName(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func ensureDeepSeekOfficialProvider(c *Config) {
	if _, ok := c.Provider("deepseek"); ok {
		return
	}
	entry := ProviderEntry{
		Name:          "deepseek",
		Kind:          "openai",
		BaseURL:       "https://api.deepseek.com",
		Models:        []string{"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"},
		Default:       "deepseek-v4-flash",
		APIKeyEnv:     "DEEPSEEK_API_KEY",
		BalanceURL:    "https://api.deepseek.com/user/balance",
		ContextWindow: 1_000_000,
	}
	if old, ok := c.Provider("deepseek-flash"); ok {
		entry = officialProviderFromLegacy(entry, old)
		entry.Models = mergeModelLists([]string{"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"}, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, "deepseek-v4-flash")
	}
	c.Providers = append(c.Providers, entry)
}

func ensureMimoAPIProvider(c *Config) {
	if existing, ok := c.Provider("mimo-api"); ok {
		if isOfficialMimoAPIEndpoint(existing.BaseURL) {
			existing.APIKeyEnv = "MIMO_API_KEY"
		}
		return
	}
	c.Providers = append(c.Providers, ProviderEntry{
		Name:          "mimo-api",
		Kind:          "openai",
		BaseURL:       "https://api.xiaomimimo.com/v1",
		Models:        []string{"mimo-v2.5", "mimo-v2.5-pro"},
		Default:       "mimo-v2.5-pro",
		APIKeyEnv:     "MIMO_API_KEY",
		ContextWindow: 1_048_576,
		NoProxy:       true,
	})
}

func ensureMimoTokenPlanProvider(c *Config, includeMimoFlash bool) {
	if existing, ok := c.Provider("mimo-token-plan"); ok {
		if isOfficialMimoTokenPlanEndpoint(existing.BaseURL) {
			existing.APIKeyEnv = "MIMO_TOKEN_PLAN_API_KEY"
		}
		return
	}
	entry := ProviderEntry{
		Name:          "mimo-token-plan",
		Kind:          "openai",
		BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
		Models:        []string{"mimo-v2.5-pro"},
		Default:       "mimo-v2.5-pro",
		APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
		ContextWindow: 1_048_576,
		NoProxy:       true,
	}
	if old, ok := c.Provider("mimo-pro"); ok {
		entry = officialProviderFromLegacy(entry, old)
		entry.Models = mergeModelLists([]string{"mimo-v2.5-pro"}, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, "mimo-v2.5-pro")
	}
	if old, ok := c.Provider("mimo-flash"); includeMimoFlash && ok {
		if !providerHasAnyModel(entry) {
			entry = officialProviderFromLegacy(entry, old)
		}
		entry.Models = mergeModelLists(entry.Models, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, entry.Default)
	}
	c.Providers = append(c.Providers, entry)
}

func isOfficialMimoAPIEndpoint(baseURL string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(baseURL), "/"), "https://api.xiaomimimo.com/v1")
}

func isOfficialMimoTokenPlanEndpoint(baseURL string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(baseURL), "/"), "https://token-plan-cn.xiaomimimo.com/v1")
}

func officialProviderFromLegacy(entry ProviderEntry, old *ProviderEntry) ProviderEntry {
	entry.Kind = old.Kind
	entry.BaseURL = old.BaseURL
	entry.ModelsURL = old.ModelsURL
	entry.APIKeyEnv = old.APIKeyEnv
	entry.BalanceURL = old.BalanceURL
	entry.ContextWindow = old.ContextWindow
	entry.Price = old.Price
	entry.Thinking = old.Thinking
	entry.Effort = old.Effort
	entry.ReasoningProtocol = old.ReasoningProtocol
	entry.SupportedEfforts = append([]string(nil), old.SupportedEfforts...)
	entry.DefaultEffort = old.DefaultEffort
	entry.NoProxy = old.NoProxy
	return entry
}

func mergeModelLists(primary, extra []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(primary)+len(extra))
	for _, list := range [][]string{primary, extra} {
		for _, model := range list {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, model)
		}
	}
	return out
}

func firstKnownModel(current string, models []string, fallback string) string {
	current = strings.TrimSpace(current)
	for _, model := range models {
		if model == current {
			return current
		}
	}
	for _, model := range models {
		if model == fallback {
			return fallback
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func retargetDesktopOfficialRefs(c *Config, access map[string]bool) {
	c.DefaultModel = retargetDesktopOfficialRef(c.DefaultModel, access)
	c.Bot.Model = retargetDesktopOfficialRef(c.Bot.Model, access)
	c.Agent.PlannerModel = retargetDesktopOfficialRef(c.Agent.PlannerModel, access)
	c.Agent.SubagentModel = retargetDesktopOfficialRef(c.Agent.SubagentModel, access)
	c.Agent.AutoPlanClassifier = retargetDesktopOfficialRef(c.Agent.AutoPlanClassifier, access)
	for skill, ref := range c.Agent.SubagentModels {
		c.Agent.SubagentModels[skill] = retargetDesktopOfficialRef(ref, access)
	}
}

func retargetDesktopOfficialRef(ref string, access map[string]bool) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	provider, model, hasModel := strings.Cut(ref, "/")
	switch provider {
	case "deepseek-flash":
		if !access["deepseek"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "deepseek-v4-flash"
		}
		return "deepseek/" + model
	case "deepseek-pro":
		if !access["deepseek"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "deepseek-v4-pro"
		}
		return "deepseek/" + model
	case "mimo-pro":
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5-pro"
		}
		if access["mimo-token-plan"] && !access["mimo-api"] {
			return "mimo-token-plan/" + model
		}
		if access["mimo-api"] && !access["mimo-token-plan"] {
			return "mimo-api/" + model
		}
		return ref
	case "mimo", "xiaomi-mimo", "xiaomi_mimo":
		if !access["mimo-api"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5-pro"
		}
		return "mimo-api/" + model
	case "mimo-flash":
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5"
		}
		if access["mimo-token-plan"] && !access["mimo-api"] {
			return "mimo-token-plan/" + model
		}
		if access["mimo-api"] && !access["mimo-token-plan"] {
			return "mimo-api/" + model
		}
		return ref
	default:
		return ref
	}
}

func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, product.ConfigDirName, "config.toml")
}

func legacyUserConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, product.LegacyConfigDirName, "config.toml")
}

// LegacyUserConfigPath exposes the read-only V2 config location to migration
// and installer tests. New writes must use UserConfigPath.
func LegacyUserConfigPath() string { return legacyUserConfigPath() }

// UserConfigPath is the user-global config file (~/.config/orca/config.toml),
// or "" when the user config dir can't be resolved.
func UserConfigPath() string { return userConfigPath() }

// UserCredentialsPath is the deepseek-orca-owned global secrets file, beside
// config.toml in the user config dir (e.g. ~/.config/orca/credentials). It
// holds KEY=value lines loaded into each Config's scoped environment by loadDotEnv. The setup
// wizard writes API keys here, deliberately NOT named .env: keys never land in a
// project's own .env (which can't be selectively gitignored), never get
// committed, and resolve from any working directory. "" when the user config dir
// can't be resolved.
func UserCredentialsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, product.ConfigDirName, "credentials")
}

func LegacyUserCredentialsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, product.LegacyConfigDirName, "credentials")
}

// ArchiveDir is where compacted conversation history is archived for
// traceability (one timestamped .jsonl per compaction). Empty if the user config
// directory cannot be resolved, in which case archiving is skipped.
func ArchiveDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, product.ConfigDirName, "archive")
}

// SessionDir is where chat sessions are persisted (one .jsonl per session).
// Used by `deepseek-orca chat --continue` / `--resume` to find the recent ones. Empty
// if the user config dir can't be resolved - sessions then aren't saved.
func SessionDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, product.ConfigDirName, "sessions")
}

// ProjectSessionDir is the per-workspace session directory the desktop sidebar
// lists: <config root>/projects/<slug>/sessions. Empty when either the config
// root or workspaceRoot doesn't resolve.
func ProjectSessionDir(workspaceRoot string) string {
	base := MemoryUserDir()
	root := strings.TrimSpace(workspaceRoot)
	if base == "" || root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Join(base, "projects", WorkspaceSlug(root), "sessions")
}

// WorkspaceSlug flattens an absolute workspace path into the directory name
// used under <config root>/projects.
func WorkspaceSlug(absPath string) string {
	return strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", ":", "-").Replace(absPath)
}

// CacheDir is the per-user cache root for derived/regenerable artefacts: MCP
// handshake snapshots, plugin startup-latency telemetry. Lives beside the
// existing O.R.C.A directories (UserConfigDir/orca/...) so the whole state tree
// shares one root the user can wipe in a single rm. Empty when the OS dir is
// unavailable - callers must tolerate that (caching is best-effort).
func CacheDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, product.ConfigDirName, "cache")
}

// MemoryUserDir returns the O.R.C.A user config root (~/.config/orca), under which
// the user-global ORCA.md and the per-project auto-memory store live. Empty
// when the user config dir can't be resolved, which disables user-scoped memory.
func MemoryUserDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, product.ConfigDirName)
}

// BotWorkspaceDir is the default isolated workspace for mobile/IM bot sessions.
// Keeping it under the O.R.C.A user root prevents bot turns from writing
// into whatever directory happened to launch the CLI.
func BotWorkspaceDir() string {
	base := MemoryUserDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "bot-workspace")
}

// ConventionDirs are the parent directories scanned for agent assets (skills,
// commands), in canonical-first order. .orca is canonical; the legacy
// .deepseek-orca directory is read-only compatibility; .agents / .agent /
// .claude let users drop in assets authored for other agent tools without moving
// files. Shared so skills (internal/skill) and commands (CommandDirs) discover
// the same set. Note: hooks are NOT scanned across these - a .claude/settings.json
// uses a different hook schema that can't be parsed as ours, so hooks stay in
// .orca/settings.json (see internal/hook).
var ConventionDirs = []string{product.ProjectStateDir, product.LegacyProjectStateDir, ".agents", ".agent", ".claude"}

// conventionSubdirsAsc joins sub under each ConventionDir of base, in ascending
// priority (reverse of ConventionDirs) so the canonical .orca ends up the
// highest-priority entry - command.Load lets a later directory win on a clash.
func conventionSubdirsAsc(base, sub string) []string {
	out := make([]string, 0, len(ConventionDirs))
	for i := len(ConventionDirs) - 1; i >= 0; i-- {
		out = append(out, filepath.Join(base, ConventionDirs[i], sub))
	}
	return out
}

// CommandDirs returns the directories scanned for custom slash commands, lowest
// priority first, so a later (more specific) directory overrides an earlier one
// on a name clash. Order: home-dir convention dirs (~/.claude/commands -> ~/.orca/commands),
// the legacy XDG user dir (~/.config/orca/commands), then the project's
// convention dirs (.claude/commands -> .orca/commands). Scanning the .claude /
// .agents / .agent dirs lets commands authored for other agent tools (same .md +
// frontmatter format) work here unchanged.
func CommandDirs() []string {
	return CommandDirsForRoot(".")
}

// CommandDirsForRoot is like CommandDirs but resolves the project convention
// dirs under root instead of the current working directory. Global (home/XDG)
// dirs are unchanged - they are always user-scoped.
func CommandDirsForRoot(root string) []string {
	root = resolveRoot(root)
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, conventionSubdirsAsc(home, "commands")...)
	}
	if dir, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(dir, product.LegacyConfigDirName, "commands"))
		dirs = append(dirs, filepath.Join(dir, product.ConfigDirName, "commands"))
	}
	dirs = append(dirs, conventionSubdirsAsc(root, "commands")...)
	return dirs
}

// SourcePath returns the highest-priority config file that exists, or "" if none.
func SourcePath() string {
	return SourcePathForRoot(".")
}

// SourcePathForRoot returns the highest-priority config file that exists under
// root, or "" if none. Equivalent to SourcePath() when root is ".".
func SourcePathForRoot(root string) string {
	root = resolveRoot(root)
	projectTOML := product.ProjectConfigName
	if root != "." {
		projectTOML = filepath.Join(root, product.ProjectConfigName)
	}
	if _, err := os.Stat(projectTOML); err == nil {
		return projectTOML
	}
	legacyProject := product.LegacyProjectConfigName
	if root != "." {
		legacyProject = filepath.Join(root, product.LegacyProjectConfigName)
	}
	if _, err := os.Stat(legacyProject); err == nil {
		return legacyProject
	}
	if uc := userConfigPath(); uc != "" {
		if _, err := os.Stat(uc); err == nil {
			return uc
		}
	}
	if legacy := legacyUserConfigPath(); legacy != "" {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return ""
}

// WriteFile writes the configuration to path as annotated TOML.
func (c *Config) WriteFile(path string) error {
	return os.WriteFile(path, []byte(RenderTOMLForScope(c, renderScopeForPath(path))), 0o644)
}

// Provider returns the named provider entry.
func (c *Config) Provider(name string) (*ProviderEntry, bool) {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i], true
		}
	}
	return nil, false
}

// ResolveModel resolves a model reference to a provider entry whose Model is the
// selected model string (a copy, so the config's lists stay intact). It accepts:
//   - "provider/model" - that exact model under that provider;
//   - a provider name   - the provider's default model;
//   - a bare model name - the (first) provider that lists it.
//
// The returned entry is ready to build a provider from (NewProvider reads .Model),
// so a single "vendor with many models" entry yields one instance per model
// without duplicating base_url/api_key_env. Single-`model` entries still resolve
// by provider name, keeping older configs working unchanged.
func (c *Config) ResolveModel(ref string) (*ProviderEntry, bool) {
	if ref == "" {
		return nil, false
	}
	access := desktopProviderAccessMap(c.Desktop.ProviderAccess)
	if len(access) > 0 {
		ref = retargetDesktopOfficialRef(ref, access)
	}
	if providerName, _, hasModel := strings.Cut(ref, "/"); hasModel &&
		(providerName == "mimo-flash" || providerName == "mimo-pro") &&
		access["mimo-api"] && access["mimo-token-plan"] {
		return nil, false
	}
	allowed := func(e *ProviderEntry) bool {
		if e == nil || len(access) == 0 {
			return e != nil
		}
		name := strings.TrimSpace(e.Name)
		canonical := canonicalDesktopOfficialProviderName(name)
		if access[canonical] {
			// Legacy official entries are retained for migration only. Once the
			// access list points at the canonical preset, model pickers and bare
			// resolution must not expose every historical alias as a duplicate.
			if name != canonical {
				if _, curated := ProviderPresetByID(canonical); curated {
					return false
				}
			}
			return true
		}
		// provider_access is an allow-list for curated official identities. A
		// user-defined OpenAI-compatible provider remains selectable by its
		// own stable ID and is never silently hidden by that list.
		_, curated := ProviderPresetByID(canonical)
		return !curated
	}
	resolved := func(e *ProviderEntry, model string) (*ProviderEntry, bool) {
		if !allowed(e) || !e.HasModel(model) {
			return nil, false
		}
		cp := *e
		cp.Model = model
		applyResolvedModelPricing(&cp)
		return &cp, true
	}
	// "provider/model"
	if prov, model, ok := strings.Cut(ref, "/"); ok {
		if e, found := c.Provider(prov); found {
			if entry, ok := resolved(e, model); ok {
				return entry, true
			}
		}
	}
	// a provider name -> its default model
	if e, found := c.Provider(ref); found && allowed(e) {
		cp := *e
		cp.Model = e.DefaultModel()
		applyResolvedModelPricing(&cp)
		return &cp, true
	}
	// A bare model name must prefer the currently selected desktop provider.
	// Legacy aliases may list the same model against another endpoint and must
	// never hijack a tab when that provider is not in provider_access.
	if len(access) > 0 {
		if prov, _, ok := strings.Cut(strings.TrimSpace(c.DefaultModel), "/"); ok {
			if e, found := c.Provider(prov); found {
				if entry, ok := resolved(e, ref); ok {
					return entry, true
				}
			}
		}
		for _, name := range c.Desktop.ProviderAccess {
			name = canonicalDesktopOfficialProviderName(name)
			if e, found := c.Provider(name); found {
				if entry, ok := resolved(e, ref); ok {
					return entry, true
				}
			}
		}
	}
	for i := range c.Providers {
		if entry, ok := resolved(&c.Providers[i], ref); ok {
			return entry, true
		}
	}
	return nil, false
}

// ResolveVisionModelRef returns the model reference for image-bearing task
// calls. An explicit vision-role override always wins and is returned as-is so
// an invalid or custom reference is not silently guessed onto another provider.
// Without that override, only a configured official DeepSeek endpoint exposing
// the known vision model is eligible for the built-in fallback. The helper is
// intentionally read-only so desktop settings can share the same resolution
// without rewriting user configuration.
func (c *Config) ResolveVisionModelRef() string {
	if c == nil {
		return ""
	}
	if ref := strings.TrimSpace(c.Agent.SubagentModels[VisionSubagentRole]); ref != "" {
		return ref
	}

	access := desktopProviderAccessMap(c.Desktop.ProviderAccess)
	var fallback string
	for i := range c.Providers {
		entry := &c.Providers[i]
		if !IsOfficialDeepSeekEntry(entry) || !entry.HasModel(OfficialDeepSeekVisionModel) {
			continue
		}
		if len(access) > 0 && !access[canonicalDesktopOfficialProviderName(entry.Name)] {
			continue
		}
		ref := strings.TrimSpace(entry.Name) + "/" + OfficialDeepSeekVisionModel
		if strings.TrimSpace(entry.Name) == "deepseek" {
			return ref
		}
		if fallback == "" {
			fallback = ref
		}
	}
	return fallback
}

// ResolveModelWithFallback resolves a model reference to the canonical
// "provider/model" form used by the desktop runtime. An unqualified stale or
// empty reference may use the configured default, but a qualified reference is
// an identity assertion and is never retried as a bare model on another
// provider.
func (c *Config) ResolveModelWithFallback(ref string) (resolvedRef string, fallback bool, ok bool) {
	if strings.TrimSpace(ref) != "" {
		if e, found := c.ResolveModel(ref); found {
			return e.Name + "/" + e.Model, false, true
		}
		if _, _, hasModel := strings.Cut(ref, "/"); hasModel {
			return "", false, false
		}
	}
	access := desktopProviderAccessMap(c.Desktop.ProviderAccess)
	if len(access) > 0 {
		for _, name := range c.Desktop.ProviderAccess {
			name = canonicalDesktopOfficialProviderName(name)
			p, found := c.Provider(name)
			if !found || len(p.ModelList()) == 0 || !p.Configured() {
				continue
			}
			return p.Name + "/" + p.DefaultModel(), true, true
		}
		return "", false, false
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		// Skip providers with no models or no API key: falling back onto a keyless
		// provider just boots the tab onto something that fails on first use. Mirrors
		// the Configured() gate the provider-removal/selection paths already apply.
		if len(p.ModelList()) == 0 || !p.Configured() {
			continue
		}
		return p.Name + "/" + p.DefaultModel(), true, true
	}
	return "", false, false
}

// WithAPIKey applies an in-memory API key for this provider entry. The
// override is intentionally private and is not persisted by TOML rendering.
func (e *ProviderEntry) WithAPIKey(token string) *ProviderEntry {
	if e == nil {
		return nil
	}
	e.apiKeyOverride = token
	e.hasAPIKeyOverride = true
	return e
}

// APIKey resolves the entry's API key from its in-memory override or
// api_key_env.
func (e *ProviderEntry) APIKey() string {
	if e == nil {
		return ""
	}
	if e.hasAPIKeyOverride {
		return e.apiKeyOverride
	}
	if e.APIKeyEnv == "" {
		return ""
	}
	value, _ := lookupScopedEnv(e.env, e.APIKeyEnv)
	return value
}

// Configured reports whether the provider's api_key_env is set - the same check
// Validate enforces, so pickers can filter on it.
func (e *ProviderEntry) Configured() bool {
	return e.APIKey() != ""
}

// ResolveSystemPrompt returns the system prompt, reading system_prompt_file if set.
func (c *Config) ResolveSystemPrompt() (string, error) {
	if c.Agent.SystemPromptFile != "" {
		b, err := os.ReadFile(c.Agent.SystemPromptFile)
		if err != nil {
			return "", fmt.Errorf("system_prompt_file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	if strings.TrimSpace(c.Agent.SystemPrompt) == "" {
		return DefaultAgentSystemPrompt, nil
	}
	return c.Agent.SystemPrompt, nil
}

// Validate checks that the selected model's provider is usable.
func (c *Config) Validate(model string) error {
	e, ok := c.ResolveModel(model)
	if !ok {
		return fmt.Errorf("unknown model %q (configured: %s)", model, c.providerNames())
	}
	if e.Kind == "" {
		return fmt.Errorf("provider %q: kind is required", model)
	}
	if e.BaseURL == "" {
		return fmt.Errorf("provider %q: base_url is required", model)
	}
	if e.APIKey() == "" {
		return fmt.Errorf("provider %q: missing env %s", model, e.APIKeyEnv)
	}
	return nil
}

func (c *Config) providerNames() string {
	names := make([]string, len(c.Providers))
	for i, p := range c.Providers {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}
