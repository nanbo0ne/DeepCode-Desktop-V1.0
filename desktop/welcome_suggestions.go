package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const (
	welcomeSuggestionInterval = 24 * time.Hour
	welcomeSuggestionSchema   = 2
)

var (
	welcomeSecretAssignment = regexp.MustCompile(`(?i)(api[_ -]?key|access[_ -]?token|authorization|password|secret)\s*[:=]\s*[^\s,;]+`)
	welcomeBearerToken      = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]+`)
	welcomePrivateKey       = regexp.MustCompile(`(?i)\b(sk|pk|key)-[a-z0-9_-]{12,}\b`)
	welcomeWindowsPath      = regexp.MustCompile(`(?i)\b[a-z]:\\[^\r\n\t"']+`)
	welcomeUnixPath         = regexp.MustCompile(`(?:^|\s)/(?:[^\s/]+/)+[^\s,;]+`)
	welcomeAssistantVoice   = regexp.MustCompile(`^(建议你|你可以考虑|可以考虑|用户可以|潜在下一步)`)
)

type welcomeSuggestionCache struct {
	Schema      int      `json:"schema"`
	Prompts     []string `json:"prompts"`
	GeneratedAt int64    `json:"generatedAt"`
	ModelRef    string   `json:"modelRef"`
	Fingerprint string   `json:"fingerprint"`
}

func welcomeSuggestionPath() string {
	return filepath.Join(desktopConfigDir(), "welcome-suggestions.json")
}

func loadWelcomeSuggestionCache() welcomeSuggestionCache {
	var cache welcomeSuggestionCache
	if data, err := os.ReadFile(welcomeSuggestionPath()); err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	if cache.Schema != welcomeSuggestionSchema || len(cache.Prompts) != 4 {
		cache.Prompts = nil
		cache.GeneratedAt = 0
	}
	return cache
}

func saveWelcomeSuggestionCache(cache welcomeSuggestionCache) error {
	if err := os.MkdirAll(desktopConfigDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := welcomeSuggestionPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, welcomeSuggestionPath())
}

func (a *App) GetWelcomeSuggestions() []string {
	return append([]string(nil), loadWelcomeSuggestionCache().Prompts...)
}

func (a *App) runWelcomeSuggestionScheduler() {
	refresh := func() {
		done, err := a.beginAppWork()
		if err != nil {
			return
		}
		defer done()
		cache := loadWelcomeSuggestionCache()
		if cache.GeneratedAt > 0 && time.Since(time.UnixMilli(cache.GeneratedAt)) < welcomeSuggestionInterval {
			return
		}
		if next, err := a.generateWelcomeSuggestions(); err == nil && len(next.Prompts) == 4 {
			_ = saveWelcomeSuggestionCache(next)
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "welcome:suggestions", next.Prompts)
			}
		}
	}
	refresh()
	ticker := time.NewTicker(welcomeSuggestionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			refresh()
		case <-a.bootContext().Done():
			return
		}
	}
}

func (a *App) generateWelcomeSuggestions() (welcomeSuggestionCache, error) {
	cfg, err := config.Load()
	if err != nil {
		return welcomeSuggestionCache{}, err
	}
	modelRef, _, ok := cfg.ResolveModelWithFallback(cfg.Bot.Model)
	if !ok {
		modelRef, _, ok = cfg.ResolveModelWithFallback(cfg.DefaultModel)
	}
	if !ok {
		return welcomeSuggestionCache{}, os.ErrNotExist
	}
	entry, ok := cfg.ResolveModel(modelRef)
	if !ok {
		return welcomeSuggestionCache{}, os.ErrNotExist
	}
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		return welcomeSuggestionCache{}, err
	}

	type row struct {
		Title     string `json:"title"`
		Workspace string `json:"workspace,omitempty"`
		Preview   string `json:"preview"`
		Updated   int64  `json:"updated"`
	}
	type styleSample struct {
		Text    string `json:"text"`
		Updated int64  `json:"updated"`
	}
	rows := make([]row, 0, 80)
	styleSamples := make([]styleSample, 0, 24)
	seen := map[string]bool{}
	for _, dir := range a.knownSessionDirs() {
		infos, listErr := agent.ListSessions(dir)
		if listErr != nil {
			continue
		}
		for _, info := range infos {
			if meta, ok, metaErr := agent.LoadBranchMeta(info.Path); metaErr == nil && ok && meta.Scope == "migration_backup" {
				continue
			}
			if seen[info.Path] || len(rows) >= 80 {
				continue
			}
			seen[info.Path] = true
			title := welcomeSuggestionText(info.TopicTitle)
			preview := welcomeSuggestionText(info.Preview)
			if title == "" && preview == "" {
				continue
			}
			if len([]rune(preview)) > 280 {
				preview = string([]rune(preview)[:280])
			}
			workspace := strings.TrimSpace(filepath.Base(strings.TrimSpace(info.WorkspaceRoot)))
			if workspace == "." || workspace == string(filepath.Separator) {
				workspace = ""
			}
			rows = append(rows, row{Title: title, Workspace: welcomeSuggestionText(workspace), Preview: preview, Updated: info.LastActivityAt.UnixMilli()})
			for _, sample := range welcomeUserStyleSamples(dir, info.Path, 2) {
				styleSamples = append(styleSamples, styleSample{Text: sample, Updated: info.LastActivityAt.UnixMilli()})
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Updated > rows[j].Updated })
	sort.SliceStable(styleSamples, func(i, j int) bool { return styleSamples[i].Updated > styleSamples[j].Updated })
	if len(styleSamples) > 16 {
		styleSamples = styleSamples[:16]
	}
	input := struct {
		Conversations []row         `json:"conversations"`
		StyleSamples  []styleSample `json:"style_samples"`
	}{Conversations: rows, StyleSamples: styleSamples}
	data, _ := json.Marshal(input)
	fingerprintBytes := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(fingerprintBytes[:])
	prompt := welcomeSuggestionPrompt(string(data))
	ctx, cancel := context.WithTimeout(a.bootContext(), 60*time.Second)
	defer cancel()
	stream, err := prov.Stream(ctx, provider.Request{Temperature: 0, MaxTokens: 500, Messages: []provider.Message{{Role: provider.RoleUser, Content: prompt}}})
	if err != nil {
		return welcomeSuggestionCache{}, err
	}
	var output strings.Builder
	for chunk := range stream {
		if chunk.Type == provider.ChunkText {
			output.WriteString(chunk.Text)
		}
		if chunk.Type == provider.ChunkError && chunk.Err != nil {
			return welcomeSuggestionCache{}, chunk.Err
		}
	}
	prompts := parseWelcomeSuggestions(output.String())
	if len(prompts) != 4 {
		return welcomeSuggestionCache{}, os.ErrInvalid
	}
	return welcomeSuggestionCache{Schema: welcomeSuggestionSchema, Prompts: prompts, GeneratedAt: time.Now().UnixMilli(), ModelRef: modelRef, Fingerprint: fingerprint}, nil
}

func welcomeSuggestionPrompt(data string) string {
	return `你正在为用户本人准备 Orca 首页的四个快捷提问。请根据 conversations 判断用户最近真正可能继续关心的事情，并模仿 style_samples 中用户自己的语气、句长、称呼和提问习惯。

生成恰好 4 条互不重复、用户此刻可能亲自对 Orca 说出的中文问题或请求：
- 必须像用户的话，而不是助手给用户的建议、任务派发或项目经理总结。
- 可以使用“帮我”“请你”“我想”等第一人称表达，并与一个真实近期话题具体相关。
- 不要使用“建议你”“可以考虑”“用户可以”“潜在下一步”等助手口吻。
- 不要机械覆盖固定分类，也不要直接复述标题；四条应体现不同但可信的当前意图。
- 不得输出凭据、完整本地路径、附件路径或隐藏上下文。
- 每条保持简短，可以直接发送。只返回 JSON 字符串数组，不要解释。

输入：` + data
}

func welcomeUserStyleSamples(dir, sessionPath string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	session, err := agent.LoadSession(sessionPath)
	if err != nil {
		return nil
	}
	resolveDisplay := sessionDisplayResolver(dir, sessionPath)
	result := make([]string, 0, limit)
	for i := len(session.Messages) - 1; i >= 0 && len(result) < limit; i-- {
		message := session.Messages[i]
		if message.Role != provider.RoleUser {
			continue
		}
		text := welcomeSuggestionText(resolveDisplay(message.Content))
		if strings.HasPrefix(text, "/") || strings.Contains(text, "[REDACTED]") || strings.Contains(text, "[本地路径]") {
			continue
		}
		runes := []rune(text)
		if len(runes) < 2 {
			continue
		}
		if len(runes) > 220 {
			text = string(runes[:220])
		}
		result = append(result, text)
	}
	return result
}

func welcomeSuggestionText(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || strings.Contains(lower, "referenced context") || strings.Contains(lower, ".orca/attachments") || strings.Contains(lower, "snapshot path") || strings.Contains(lower, "<image ") {
			continue
		}
		trimmed = welcomeBearerToken.ReplaceAllString(trimmed, "Bearer [REDACTED]")
		trimmed = welcomeSecretAssignment.ReplaceAllString(trimmed, "$1=[REDACTED]")
		trimmed = welcomePrivateKey.ReplaceAllString(trimmed, "[REDACTED]")
		trimmed = welcomeWindowsPath.ReplaceAllString(trimmed, "[本地路径]")
		trimmed = welcomeUnixPath.ReplaceAllString(trimmed, " [本地路径]")
		kept = append(kept, strings.TrimSpace(trimmed))
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

func parseWelcomeSuggestions(raw string) []string {
	start, end := strings.Index(raw, "["), strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil
	}
	var values []string
	if json.Unmarshal([]byte(raw[start:end+1]), &values) != nil || len(values) != 4 {
		return nil
	}
	seen := map[string]bool{}
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
		if values[i] == "" || seen[values[i]] || len([]rune(values[i])) > 60 || welcomeAssistantVoice.MatchString(values[i]) {
			return nil
		}
		seen[values[i]] = true
	}
	return values
}
