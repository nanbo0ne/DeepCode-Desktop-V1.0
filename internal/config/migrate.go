package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// legacyConfig is the subset of the v0.x (~/.orca/config.json) schema this
// import carries forward. Fields absent here are dropped on purpose: desktop tab
// state is frontend-owned, and skills already live in the shared ~/.orca/skills
// root that v1+ also scans, so they need no migration.
type legacyConfig struct {
	APIKey      string                       `json:"apiKey"`
	BaseURL     string                       `json:"baseUrl"`
	Lang        string                       `json:"lang"`
	MCP         []string                     `json:"mcp"` // pre-mcpServers `--mcp`-format strings
	MCPServers  map[string]legacyMCPServer   `json:"mcpServers"`
	MCPEnv      map[string]map[string]string `json:"mcpEnv"`
	MCPDisabled []string                     `json:"mcpDisabled"`
}

type legacyMCPServer struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	Transport string            `json:"transport"`
	Type      string            `json:"type"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Disabled  bool              `json:"disabled"`
}

// MigrationResult summarizes a one-time legacy import for the boot-time notice.
type MigrationResult struct {
	From     string
	To       string
	KeyToEnv bool
	Plugins  int
	Warnings []string
}

const legacyMigrationMarkerSuffix = ".legacy-migration.json"

type legacyMigrationRecord struct {
	Version            int    `json:"version"`
	Source             string `json:"source"`
	Destination        string `json:"destination"`
	ConfigWritten      bool   `json:"configWritten"`
	CredentialsWritten bool   `json:"credentialsWritten"`
	Complete           bool   `json:"complete"`
}

func (r *MigrationResult) Notice() string {
	var b strings.Builder
	fmt.Fprintf(&b, "migrated your previous configuration: %s → %s", r.From, r.To)
	if r.Plugins > 0 {
		fmt.Fprintf(&b, " (%d MCP server(s))", r.Plugins)
	}
	if r.KeyToEnv {
		b.WriteString("; API key saved to deepseek-orca's credentials store")
	}
	b.WriteString(". The old files were left untouched.")
	for _, w := range r.Warnings {
		b.WriteString("\n  note: " + w)
	}
	return b.String()
}

// MigrateLegacyIfNeeded performs a one-time, non-destructive import of older
// installs into the current user config when the latter does not exist yet. It
// checks v1-era TOML first, then v0.5/v0.x ~/.orca/config.json, and never
// modifies or deletes the legacy files. Returns nil when there is nothing to
// migrate, or when the current user config already exists.
func MigrateLegacyIfNeeded() (*MigrationResult, error) {
	dest := userConfigPath()
	if dest == "" {
		return nil, nil
	}
	markerPath := dest + legacyMigrationMarkerSuffix
	if record, err := readLegacyMigrationRecord(markerPath); err == nil {
		if record.Complete {
			return nil, nil
		}
		return resumeLegacyJSONMigration(dest, markerPath, record)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if _, err := os.Stat(dest); err == nil {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	if res, err := migrateLegacyTOMLIfNeeded(dest, home); res != nil || err != nil {
		return res, err
	}
	src := filepath.Join(home, ".deepseek-orca", "config.json")
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, nil
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // tolerate a UTF-8 BOM (some editors add one)
	var legacy legacyConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parse legacy config %s: %w", src, err)
	}
	return migrateLegacyJSON(dest, markerPath, src, legacy, home)
}

func migrateLegacyJSON(dest, markerPath, src string, legacy legacyConfig, home string) (*MigrationResult, error) {

	cfg := Default()
	res := &MigrationResult{From: src, To: dest}
	if legacy.Lang != "" {
		cfg.Language = legacy.Lang
		_ = cfg.SetDesktopLanguage(legacy.Lang)
	}
	migrateLegacyBaseURL(cfg, legacy.BaseURL)
	cfg.Plugins = legacyPlugins(legacy)
	res.Plugins = len(cfg.Plugins)

	var envLines []string
	if key := strings.TrimSpace(legacy.APIKey); key != "" {
		envLines = append(envLines, "DEEPSEEK_API_KEY="+key)
		res.KeyToEnv = true
		if base := strings.TrimSpace(legacy.BaseURL); base != "" && !strings.Contains(base, "deepseek.com") {
			res.Warnings = append(res.Warnings, "your previous base_url was "+base+
				" — it was applied to the built-in DeepSeek providers; verify models if this endpoint is not DeepSeek-compatible")
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	record := legacyMigrationRecord{Version: 1, Source: src, Destination: dest}
	if err := writeLegacyMigrationRecord(markerPath, record); err != nil {
		return nil, fmt.Errorf("write migration state: %w", err)
	}
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		body := []byte(RenderTOMLForScope(cfg, renderScopeForPath(dest)))
		if err := atomicWriteMigrationFile(dest, body, 0o644); err != nil {
			return res, fmt.Errorf("write %s: %w", dest, err)
		}
	} else if err != nil {
		return res, fmt.Errorf("stat %s: %w", dest, err)
	}
	record.ConfigWritten = true
	if err := writeLegacyMigrationRecord(markerPath, record); err != nil {
		return res, fmt.Errorf("write migration state: %w", err)
	}
	return finishLegacyJSONMigration(res, markerPath, record, envLines, home)
}

func resumeLegacyJSONMigration(dest, markerPath string, record legacyMigrationRecord) (*MigrationResult, error) {
	if record.Version != 1 || filepath.Clean(record.Destination) != filepath.Clean(dest) || strings.TrimSpace(record.Source) == "" {
		return nil, fmt.Errorf("invalid legacy migration state %s", markerPath)
	}
	data, err := os.ReadFile(record.Source)
	if err != nil {
		return nil, fmt.Errorf("read legacy config %s: %w", record.Source, err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var legacy legacyConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parse legacy config %s: %w", record.Source, err)
	}
	res := &MigrationResult{From: record.Source, To: dest, Plugins: len(legacyPlugins(legacy))}
	if strings.TrimSpace(legacy.APIKey) != "" {
		res.KeyToEnv = true
	}
	if !record.ConfigWritten {
		if info, statErr := os.Stat(dest); statErr == nil && !info.IsDir() {
			record.ConfigWritten = true
		} else if os.IsNotExist(statErr) {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return res, homeErr
			}
			return migrateLegacyJSON(dest, markerPath, record.Source, legacy, home)
		} else if statErr != nil {
			return res, fmt.Errorf("stat %s: %w", dest, statErr)
		} else {
			return res, fmt.Errorf("migration destination %s is a directory", dest)
		}
	}
	if err := writeLegacyMigrationRecord(markerPath, record); err != nil {
		return res, fmt.Errorf("write migration state: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return res, err
	}
	return finishLegacyJSONMigration(res, markerPath, record, legacyAPIKeyLines(legacy), home)
}

func legacyAPIKeyLines(legacy legacyConfig) []string {
	if key := strings.TrimSpace(legacy.APIKey); key != "" {
		return []string{"DEEPSEEK_API_KEY=" + key}
	}
	return nil
}

func finishLegacyJSONMigration(res *MigrationResult, markerPath string, record legacyMigrationRecord, envLines []string, home string) (*MigrationResult, error) {
	if !record.CredentialsWritten && len(envLines) > 0 {
		if err := writeCredentialsEnv(home, envLines); err != nil {
			return res, fmt.Errorf("write credentials: %w", err)
		}
	}
	record.CredentialsWritten = true
	record.Complete = true
	if err := writeLegacyMigrationRecord(markerPath, record); err != nil {
		return res, fmt.Errorf("write migration state: %w", err)
	}
	return res, nil
}

func writeLegacyMigrationRecord(path string, record legacyMigrationRecord) error {
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteMigrationFile(path, append(body, '\n'), 0o600)
}

func readLegacyMigrationRecord(path string) (legacyMigrationRecord, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return legacyMigrationRecord{}, err
	}
	var record legacyMigrationRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return legacyMigrationRecord{}, err
	}
	return record, nil
}

func migrateLegacyTOMLIfNeeded(dest, home string) (*MigrationResult, error) {
	for _, src := range legacyTOMLPaths(dest, home) {
		if src == "" || filepath.Clean(src) == filepath.Clean(dest) {
			continue
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		cfg := Default()
		if err := mergeFile(cfg, src); err != nil {
			return nil, fmt.Errorf("parse legacy config %s: %w", src, err)
		}
		cfg.ConfigVersion = Default().ConfigVersion
		if strings.TrimSpace(cfg.Desktop.CloseBehavior) == "" && strings.TrimSpace(cfg.UI.CloseBehavior) != "" {
			cfg.Desktop.CloseBehavior = cfg.DesktopCloseBehavior()
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, fmt.Errorf("create config dir: %w", err)
		}
		if err := cfg.WriteFile(dest); err != nil {
			return nil, fmt.Errorf("write %s: %w", dest, err)
		}
		return &MigrationResult{From: src, To: dest, Plugins: len(cfg.Plugins)}, nil
	}
	return nil, nil
}

func legacyTOMLPaths(dest, home string) []string {
	paths := []string{filepath.Join(filepath.Dir(dest), "deepseek-orca.toml")}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".deepseek-orca", "deepseek-orca.toml"))
	}
	return paths
}

func migrateLegacyBaseURL(cfg *Config, baseURL string) {
	baseURL = strings.TrimSpace(baseURL)
	if cfg == nil || baseURL == "" {
		return
	}
	for i := range cfg.Providers {
		if cfg.Providers[i].APIKeyEnv == "DEEPSEEK_API_KEY" {
			cfg.Providers[i].BaseURL = baseURL
		}
	}
}

func legacyPlugins(legacy legacyConfig) []PluginEntry {
	disabled := make(map[string]bool, len(legacy.MCPDisabled))
	for _, n := range legacy.MCPDisabled {
		disabled[n] = true
	}
	var out []PluginEntry
	index := make(map[string]int)
	add := func(pe PluginEntry, off bool) {
		if off {
			v := false
			pe.AutoStart = &v
		}
		pe, _ = NormalizePluginCommandLine(pe)
		if j, dup := index[pe.Name]; dup {
			out[j] = pe // mcpServers overrides the `mcp` list on a name collision, matching v0.x
			return
		}
		index[pe.Name] = len(out)
		out = append(out, pe)
	}
	for i, raw := range legacy.MCP {
		pe, ok := parseLegacyMCPSpec(raw)
		if !ok {
			continue
		}
		if pe.Name == "" {
			pe.Name = anonymousMCPName(i)
		} else if pe.Command != "" {
			pe.Env = mergeEnv(nil, legacy.MCPEnv[pe.Name])
		}
		add(pe, disabled[pe.Name])
	}
	names := make([]string, 0, len(legacy.MCPServers))
	for n := range legacy.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		s := legacy.MCPServers[name]
		pe := PluginEntry{
			Name:    name,
			Type:    normalizeTransport(firstNonEmpty(s.Type, s.Transport)),
			Command: s.Command,
			Args:    s.Args,
			Env:     mergeEnv(s.Env, legacy.MCPEnv[name]),
			URL:     s.URL,
			Headers: s.Headers,
		}
		add(pe, s.Disabled || disabled[name])
	}
	return out
}

// normalizeTransport maps the v0.x transport names to v1+ plugin types. stdio is
// the default, so it returns "" (RenderTOML then omits the field).
func normalizeTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	default:
		return ""
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// mergeEnv overlays the per-server env map onto the spec's own env (overlay wins,
// matching v0.x mcpEnv precedence). Returns nil when both are empty.
func mergeEnv(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// writeCredentialsEnv merges lines into the deepseek-orca-owned global credentials
// file (UserCredentialsPath, e.g. %AppData%\deepseek-orca\credentials), keeping any
// existing assignment of the same key so a retry cannot overwrite a newer value.
// The current config resolves the imported value from this credentials file
// without changing the process environment. Falls back to
// ~/.env only when the user config dir can't be resolved — never a project .env,
// so a migration keeps secrets out of the user's project tree.
func writeCredentialsEnv(home string, lines []string) error {
	path := UserCredentialsPath()
	if path == "" {
		path = filepath.Join(home, ".env")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	target := make(map[string]bool, len(lines))
	for _, l := range lines {
		if k, _, ok := strings.Cut(l, "="); ok {
			target[strings.TrimSpace(k)] = true
		}
	}
	var kept []string
	existing := make(map[string]bool)
	if data, err := os.ReadFile(path); err == nil {
		for _, raw := range strings.Split(string(data), "\n") {
			check := strings.TrimPrefix(strings.TrimSpace(raw), "export ")
			if k, _, ok := strings.Cut(check, "="); ok {
				key := strings.TrimSpace(k)
				if target[key] {
					existing[key] = true
				}
			}
			kept = append(kept, raw)
		}
		if n := len(kept); n > 0 && kept[n-1] == "" {
			kept = kept[:n-1]
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	var b strings.Builder
	for _, l := range kept {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	for _, l := range lines {
		key, _, ok := strings.Cut(l, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || existing[key] {
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
