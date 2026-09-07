package boot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/memory"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/plugin"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/sandbox"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool/builtin"

	// Blank import registers the provider kind the same way cmd/deepseek-orca's main
	// does; importing builtin above registers the built-in tools.
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
)

type profileTestTool struct{ name string }

func (t profileTestTool) Name() string                                           { return t.name }
func (profileTestTool) Description() string                                      { return "profile boundary test" }
func (profileTestTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (profileTestTool) Execute(context.Context, json.RawMessage) (string, error) { return "ok", nil }
func (profileTestTool) ReadOnly() bool                                           { return true }

// TestBuildFoldsProjectMemoryIntoSystemPrompt is the end-to-end proof of the
// cache-first wiring: a project DEEPSEEK_ORCA.md is discovered at boot and folded
// into the session's system message (the cached prefix), and the `remember`
// tool is registered. It builds a real Controller from a throwaway project dir.
func TestBuildFoldsProjectMemoryIntoSystemPrompt(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)
	writeFile(t, dir, "DEEPSEEK_ORCA.md", "Project rule: always run go vet before committing.")

	ctrl, err := Build(context.Background(), Options{}) // RequireKey false: no network/key needed
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	// The system message is the cached prefix; it must contain both the base
	// prompt and the discovered memory.
	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "BASE SYSTEM PROMPT") {
		t.Fatalf("base prompt missing from system message:\n%s", sys)
	}
	if !strings.Contains(sys, "always run go vet before committing") {
		t.Fatalf("project DEEPSEEK_ORCA.md not folded into system message:\n%s", sys)
	}
	// Base must come first so it stays a valid cache prefix when memory changes.
	if strings.Index(sys, "BASE SYSTEM PROMPT") > strings.Index(sys, "always run go vet") {
		t.Fatalf("memory should follow the base prompt, not precede it:\n%s", sys)
	}

	if mem := ctrl.Memory(); mem == nil || len(mem.Docs) == 0 {
		t.Fatal("controller memory set is empty after discovering DEEPSEEK_ORCA.md")
	}
}

func TestBuildPromptProfilesShareTaskTrackingAndRoutingPolicies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enhanced bool
	}{
		{name: "normal", enhanced: false},
		{name: "enhanced", enhanced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)

			ctrl, err := Build(context.Background(), Options{EnhancedMode: tc.enhanced})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()

			sys := systemMessage(ctrl.History())
			for _, want := range []string{
				config.TaskTrackingPolicy,
				config.ActiveToolRoutingPolicy,
				config.ActiveLanguagePolicy,
				"todo_write",
				"automation_create",
				"Be evidence-first",
				"Use web_search when you do not know the URL",
			} {
				if !strings.Contains(sys, want) {
					t.Fatalf("%s prompt missing %q:\n%s", tc.name, want, sys)
				}
			}
			if strings.Contains(sys, "任务跟踪规则") {
				t.Fatalf("%s prompt should use the English task tracking policy, got:\n%s", tc.name, sys)
			}
			for _, forbidden := range []string{"ToolSearch", "Google Search", "TaskCreate", "CronCreate", "PushNotification"} {
				if strings.Contains(sys, forbidden) {
					t.Fatalf("%s prompt should not contain unavailable tool %q:\n%s", tc.name, forbidden, sys)
				}
			}
		})
	}
}

func TestBuildAssistantPromptModeUsesWorkProfile(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{PromptMode: PromptModeAssistant})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	for _, want := range []string{"Assistant mode", "<tone_and_formatting>", config.TaskTrackingPolicy, "Work tool use and evidence policy:", config.ActiveLanguagePolicy, "artifact_create"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("assistant prompt missing %q:\n%s", want, sys)
		}
	}
	for _, forbidden := range []string{"Claude", "Anthropic", "OpenAI", "Codex", "ToolSearch", "Google Search", "TaskCreate", "CronCreate", "PushNotification", "use bash mainly for builds"} {
		if strings.Contains(sys, forbidden) {
			t.Fatalf("assistant prompt should not contain %q:\n%s", forbidden, sys)
		}
	}
}

func TestBuildOrcaRuntimeProfileAddsConversationRouting(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)
	ctrl, err := Build(context.Background(), Options{RuntimeProfile: RuntimeProfileOrca})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	sys := systemMessage(ctrl.History())
	for _, want := range []string{"You are Orca, the fixed control conversation", "conversation_dispatch", "conversation_wait"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("Orca prompt missing %q", want)
		}
	}
}

func TestRuntimeProfilesEnforceToolBoundaries(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)
	build := func(profile string) *control.Controller {
		t.Helper()
		ctrl, err := Build(context.Background(), Options{
			RuntimeProfile: profile,
			ExtraTools:     []tool.Tool{profileTestTool{name: "conversation_dispatch"}},
		})
		if err != nil {
			t.Fatalf("Build(%s): %v", profile, err)
		}
		t.Cleanup(ctrl.Close)
		return ctrl
	}
	has := func(ctrl *control.Controller, name string) bool {
		for _, candidate := range ctrl.ToolNames() {
			if candidate == name {
				return true
			}
		}
		return false
	}
	coding := build(RuntimeProfileCoding)
	assistant := build(RuntimeProfileAssistant)
	orca := build(RuntimeProfileOrca)
	if !has(coding, "bash") || has(coding, "artifact_create") || has(coding, "conversation_dispatch") {
		t.Fatalf("coding tools violate profile boundary: %v", coding.ToolNames())
	}
	if has(assistant, "bash") || has(assistant, "bash_output") || has(assistant, "kill_shell") || !has(assistant, "artifact_create") || has(assistant, "conversation_dispatch") {
		t.Fatalf("assistant tools violate profile boundary: %v", assistant.ToolNames())
	}
	if has(orca, "bash") || has(orca, "bash_output") || has(orca, "kill_shell") || !has(orca, "artifact_create") || !has(orca, "conversation_dispatch") {
		t.Fatalf("Orca tools violate profile boundary: %v", orca.ToolNames())
	}
}

func TestBuildMemoryProfilesByPromptMode(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)
	userDir := config.MemoryUserDir()
	if _, err := memory.StoreFor(userDir, dir).Save(memory.Memory{Name: "shared-fact", Description: "shared detail", Body: "shared body"}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.AssistantStoreFor(userDir, dir).Save(memory.Memory{Name: "assistant-fact", Description: "assistant detail", Body: "assistant body"}); err != nil {
		t.Fatal(err)
	}

	assistantCtrl, err := Build(context.Background(), Options{PromptMode: PromptModeAssistant})
	if err != nil {
		t.Fatalf("Build assistant: %v", err)
	}
	defer assistantCtrl.Close()
	assistantMem := assistantCtrl.Memory()
	if assistantMem.Profile != memory.ProfileAssistant || !strings.Contains(assistantMem.Index, "assistant-fact.md") || strings.Contains(assistantMem.Index, "shared-fact.md") {
		t.Fatalf("assistant memory profile = %q index=%q, want assistant only", assistantMem.Profile, assistantMem.Index)
	}

	normalCtrl, err := Build(context.Background(), Options{PromptMode: PromptModeNormal})
	if err != nil {
		t.Fatalf("Build normal: %v", err)
	}
	defer normalCtrl.Close()
	normalMem := normalCtrl.Memory()
	if normalMem.Profile != memory.ProfileSharedAgent || strings.Contains(normalMem.Index, "assistant-fact.md") || !strings.Contains(normalMem.Index, "shared-fact.md") {
		t.Fatalf("coding memory profile = %q index=%q, want shared-agent only", normalMem.Profile, normalMem.Index)
	}
	if normalMem.Store.Dir != normalMem.SharedStore.Dir {
		t.Fatalf("normal writable store = %q, want shared %q", normalMem.Store.Dir, normalMem.SharedStore.Dir)
	}

	engineeringCtrl, err := Build(context.Background(), Options{PromptMode: PromptModeNormal, MemoryProfile: memory.ProfileSharedAgent})
	if err != nil {
		t.Fatalf("Build engineering memory profile: %v", err)
	}
	defer engineeringCtrl.Close()
	if engineeringCtrl.Memory().Profile != memory.ProfileSharedAgent || strings.Contains(engineeringCtrl.Memory().Index, "assistant-fact.md") {
		t.Fatalf("explicit shared memory profile = %q index=%q, want shared-agent without assistant memory", engineeringCtrl.Memory().Profile, engineeringCtrl.Memory().Index)
	}
}

func TestBuildPromptRespectsToolLibrarySettings(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[tool_library]
web_search_enabled = false
proactive_tool_use_enabled = false

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	sys := systemMessage(ctrl.History())
	for _, forbidden := range []string{"Be evidence-first", "Use web_search"} {
		if strings.Contains(sys, forbidden) {
			t.Fatalf("prompt should not contain disabled policy %q:\n%s", forbidden, sys)
		}
	}
	if !strings.Contains(sys, "Use web_fetch only when you already have a specific URL") {
		t.Fatalf("prompt missing web_fetch fallback:\n%s", sys)
	}
}

func TestBuildSubagentSkillFailedContinuationPersistsTranscript(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootSubagentTestProvider()
	prov := &bootSubagentTestProvider{}
	setBootSubagentTestProvider(t, prov)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-subagent-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	sessionPath := agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label())
	ctrl.SetSessionPath(sessionPath)

	if err := ctrl.Run(context.Background(), "first review"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	ref := subagentRefFromHistory(t, ctrl.History())
	prov.setContinueRef(ref)

	if err := ctrl.Run(context.Background(), "continue review"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	store := agent.NewSubagentStore(filepath.Join(config.SessionDir(), "subagents"))
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != agent.SubagentFailed {
		t.Fatalf("status = %q, want failed", meta.Status)
	}
	if meta.ParentSession != agent.BranchID(sessionPath) {
		t.Fatalf("parent session = %q, want %q", meta.ParentSession, agent.BranchID(sessionPath))
	}
	sess, err := agent.LoadSession(filepath.Join(config.SessionDir(), "subagents", ref+".jsonl"))
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	msgs := sess.Snapshot()
	if len(msgs) != 4 || msgs[1].Content != "first skill task" || msgs[2].Content != "first skill answer" || msgs[3].Content != "second skill task" {
		t.Fatalf("failed skill transcript = %+v, want first task/answer plus second task", msgs)
	}
}

const bootSubagentTestProviderKind = "boot-subagent-test"

var (
	bootSubagentTestProviderOnce    sync.Once
	bootSubagentTestProviderCurrent *bootSubagentTestProvider
	bootSubagentTestProviderMu      sync.Mutex
)

func registerBootSubagentTestProvider() {
	bootSubagentTestProviderOnce.Do(func() {
		provider.Register(bootSubagentTestProviderKind, func(provider.Config) (provider.Provider, error) {
			bootSubagentTestProviderMu.Lock()
			defer bootSubagentTestProviderMu.Unlock()
			if bootSubagentTestProviderCurrent == nil {
				return nil, errors.New("boot subagent test provider is not installed")
			}
			return bootSubagentTestProviderCurrent, nil
		})
	})
}

func setBootSubagentTestProvider(t *testing.T, p *bootSubagentTestProvider) {
	t.Helper()
	bootSubagentTestProviderMu.Lock()
	bootSubagentTestProviderCurrent = p
	bootSubagentTestProviderMu.Unlock()
	t.Cleanup(func() {
		bootSubagentTestProviderMu.Lock()
		if bootSubagentTestProviderCurrent == p {
			bootSubagentTestProviderCurrent = nil
		}
		bootSubagentTestProviderMu.Unlock()
	})
}

type bootSubagentTestProvider struct {
	mu          sync.Mutex
	calls       int
	continueRef string
}

func (p *bootSubagentTestProvider) Name() string { return "boot-subagent-test" }

func (p *bootSubagentTestProvider) setContinueRef(ref string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.continueRef = ref
}

func (p *bootSubagentTestProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	ref := p.continueRef
	p.mu.Unlock()

	var chunks []provider.Chunk
	switch call {
	case 0:
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "review-1", Name: "review", Arguments: `{"task":"first skill task"}`}}}
	case 1:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "first skill answer"}, {Type: provider.ChunkDone}}
	case 2:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "parent first done"}, {Type: provider.ChunkDone}}
	case 3:
		args, _ := json.Marshal(map[string]string{"task": "second skill task", "continue_from": ref})
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "review-2", Name: "review", Arguments: string(args)}}}
	case 4:
		chunks = []provider.Chunk{{Type: provider.ChunkError, Err: errors.New("subagent skill failed")}}
	case 5:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "parent second done"}, {Type: provider.ChunkDone}}
	case 6:
		// A failed subagent tool is a hard readiness signal. The host permits one
		// targeted follow-up, then accepts the answer when no new failure appears.
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "parent second done after verification"}, {Type: provider.ChunkDone}}
	default:
		chunks = []provider.Chunk{{Type: provider.ChunkError, Err: fmt.Errorf("unexpected provider call %d", call)}}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func subagentRefFromHistory(t *testing.T, msgs []provider.Message) string {
	t.Helper()
	for _, msg := range msgs {
		if msg.Role != provider.RoleTool {
			continue
		}
		for _, line := range strings.Split(msg.Content, "\n") {
			if strings.HasPrefix(line, "Subagent reference: ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Subagent reference: "))
			}
		}
	}
	t.Fatalf("no subagent reference in history: %+v", msgs)
	return ""
}

// TestBuildHeadlessRunRunsTaskSubagentWithoutSessionPath reproduces headless
// `deepseek-orca run`: a controller built via Build with NO SetSessionPath (exactly
// what internal/cli.runAgent does) must still be able to run a `task` sub-agent.
// Before the ephemeral fallback this failed with "parent session is required".
func TestBuildHeadlessRunRunsTaskSubagentWithoutSessionPath(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerHeadlessTaskTestProvider()
	prov := &headlessTaskTestProvider{}
	setHeadlessTaskTestProvider(t, prov)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-headless-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	// Deliberately NOT calling SetSessionPath — this is the headless run path.
	if err := ctrl.Run(context.Background(), "use a task subagent"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := ctrl.SessionPath(); got != "" {
		t.Fatalf("headless run should keep an empty session path, got %q", got)
	}

	var toolContent string
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool {
			toolContent += "\n" + msg.Content
		}
	}
	if strings.Contains(toolContent, "parent session is required") {
		t.Fatalf("task subagent failed in headless run mode: %s", toolContent)
	}
	if !strings.Contains(toolContent, "subagent answer") {
		t.Fatalf("task tool result = %q, want sub-agent answer", toolContent)
	}
	if strings.Contains(toolContent, "Subagent reference") {
		t.Fatalf("ephemeral headless run should not persist a transcript reference: %s", toolContent)
	}
}

const headlessTaskTestProviderKind = "boot-headless-test"

var (
	headlessTaskTestProviderOnce    sync.Once
	headlessTaskTestProviderCurrent *headlessTaskTestProvider
	headlessTaskTestProviderMu      sync.Mutex
)

func registerHeadlessTaskTestProvider() {
	headlessTaskTestProviderOnce.Do(func() {
		provider.Register(headlessTaskTestProviderKind, func(provider.Config) (provider.Provider, error) {
			headlessTaskTestProviderMu.Lock()
			defer headlessTaskTestProviderMu.Unlock()
			if headlessTaskTestProviderCurrent == nil {
				return nil, errors.New("headless task test provider is not installed")
			}
			return headlessTaskTestProviderCurrent, nil
		})
	})
}

func setHeadlessTaskTestProvider(t *testing.T, p *headlessTaskTestProvider) {
	t.Helper()
	headlessTaskTestProviderMu.Lock()
	headlessTaskTestProviderCurrent = p
	headlessTaskTestProviderMu.Unlock()
	t.Cleanup(func() {
		headlessTaskTestProviderMu.Lock()
		if headlessTaskTestProviderCurrent == p {
			headlessTaskTestProviderCurrent = nil
		}
		headlessTaskTestProviderMu.Unlock()
	})
}

type headlessTaskTestProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *headlessTaskTestProvider) Name() string { return "boot-headless-test" }

func (p *headlessTaskTestProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.mu.Unlock()

	var chunks []provider.Chunk
	switch call {
	case 0:
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "task-1", Name: "task", Arguments: `{"prompt":"find callers"}`}}}
	case 1:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "subagent answer"}, {Type: provider.ChunkDone}}
	default:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "parent done"}, {Type: provider.ChunkDone}}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func TestNewProviderAppliesConfiguredDefaultEffort(t *testing.T) {
	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	p, err := NewProvider(&config.ProviderEntry{
		Name:             "custom",
		Kind:             "openai",
		BaseURL:          srv.URL,
		Model:            "m",
		SupportedEfforts: []string{"low", "medium", "high"},
		DefaultEffort:    "MEDIUM",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got := gotReq["reasoning_effort"]; got != "medium" {
		t.Fatalf("reasoning_effort = %#v, want medium from default_effort", got)
	}
}

func TestNewProviderAppliesModelReasoningProtocol(t *testing.T) {
	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	p, err := NewProvider(&config.ProviderEntry{
		Name:    "deepseek-proxy",
		Kind:    "openai",
		BaseURL: srv.URL,
		Model:   "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got := gotReq["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high from DeepSeek model capability", got)
	}
	thinking, ok := gotReq["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v, want enabled", gotReq["thinking"])
	}
}

func TestBuildHonorsSessionDirOverride(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)

	sessionDir := filepath.Join(t.TempDir(), "desktop-workspace-sessions")
	ctrl, err := Build(context.Background(), Options{SessionDir: sessionDir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if got := ctrl.SessionDir(); got != sessionDir {
		t.Fatalf("SessionDir() = %q, want override %q", got, sessionDir)
	}
}

// TestBuildDiscoversSkills proves the skill wiring end-to-end: a project skill
// is discovered at boot, surfaced via Controller.Skills(), and its name folds
// into the cache-stable system prompt's "# Skills" index alongside a built-in.
func TestBuildDiscoversSkills(t *testing.T) {
	dir := robustTempDir(t)
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)
	writeFile(t, dir, ".orca/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	var hasProj, hasBuiltin bool
	for _, s := range ctrl.Skills() {
		switch s.Name {
		case "projskill":
			hasProj = true
		case "explore":
			hasBuiltin = true
		}
	}
	if !hasProj || !hasBuiltin {
		t.Fatalf("Skills() should include the project skill and a built-in; got %v", ctrl.Skills())
	}

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "# Skills") {
		t.Fatalf("skills index missing from system prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "projskill") || !strings.Contains(sys, "explore") {
		t.Fatalf("skill names missing from index:\n%s", sys)
	}
}

func TestAddBuiltinsWithWorkspaceRootKeepsSessionTools(t *testing.T) {
	reg := tool.NewRegistry()
	var stderr bytes.Buffer
	addBuiltins(reg, nil, []string{robustTempDir(t)}, sandbox.Spec{}, 120*time.Second, builtin.SearchSpec{}, &stderr, robustTempDir(t), netclient.ProxySpec{}, config.BuildBashHostToolSteer(config.DefaultToolLibrarySettings()))
	for _, name := range []string{
		"todo_write",
		"complete_step",
		"bash_output",
		"kill_shell",
		"wait",
		"notebook_edit",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("workspace builtins missing %q; got %v", name, reg.Names())
		}
	}
}

func TestBuildOmitsDisabledSkillsFromPromptAndRuntimeList(t *testing.T) {
	dir := robustTempDir(t)
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[skills]
disabled_skills = ["projskill", "review"]

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)
	writeFile(t, dir, ".orca/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, s := range ctrl.Skills() {
		if s.Name == "projskill" || s.Name == "review" {
			t.Fatalf("disabled skill %q should not be executable: %v", s.Name, ctrl.Skills())
		}
	}
	var allHasProj bool
	for _, s := range ctrl.AllSkills() {
		if s.Name == "projskill" {
			allHasProj = true
		}
	}
	if !allHasProj {
		t.Fatalf("AllSkills should include disabled skills for management: %v", ctrl.AllSkills())
	}
	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, "projskill") || strings.Contains(sys, "- review ") {
		t.Fatalf("disabled skill names should be omitted from system prompt:\n%s", sys)
	}
}

func TestBuildOmitsExcludedSkillRootsFromPromptAndRuntimeList(t *testing.T) {
	dir := robustTempDir(t)
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(dir)
	excluded := filepath.Join(home, ".agents", "skills")
	writeFile(t, dir, ".orca/skills/keep.md", "---\ndescription: keep\n---\nplaybook")
	writeFile(t, home, ".agents/skills/noisy.md", "---\ndescription: noisy\n---\nplaybook")
	writeFile(t, dir, "deepseek-orca.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[skills]
excluded_paths = [%q]

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`, excluded))

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, s := range ctrl.Skills() {
		if s.Name == "noisy" {
			t.Fatalf("excluded skill should not be executable: %v", ctrl.Skills())
		}
	}
	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, "noisy") {
		t.Fatalf("excluded skill name should be omitted from system prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "- keep") {
		t.Fatalf("non-excluded skill should remain in system prompt:\n%s", sys)
	}
}

// Custom instructions append after the stable normal-mode core. Memory remains
// absent when no memory files exist.
func TestBuildWithoutMemoryAppendsCustomPromptToNormalCore(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "JUST THE BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "You are Orca, a coding agent") {
		t.Fatalf("normal core prompt missing:\n%s", sys)
	}
	if !strings.Contains(sys, "# User-configured instructions\n\nJUST THE BASE") {
		t.Fatalf("custom instructions were not appended:\n%s", sys)
	}
}

func TestBuildLanguagePolicyIsAppended(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, config.ActiveLanguagePolicy) {
		t.Fatalf("language policy missing from system prompt:\n%s", sys)
	}
}

func systemMessage(msgs []provider.Message) string {
	for _, m := range msgs {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

func stripLanguagePolicy(s string) string {
	s = strings.TrimSpace(s)
	for _, policy := range []string{
		config.ActiveLanguagePolicy,
		config.LanguagePolicy,
	} {
		s = strings.TrimSpace(strings.TrimSuffix(s, policy))
	}
	return s
}

func stripToolRoutingPolicy(s string) string {
	s = strings.TrimSpace(s)
	for _, policy := range []string{config.ActiveToolRoutingPolicy, config.ToolRoutingPolicy} {
		s = strings.TrimSpace(strings.TrimSuffix(s, policy))
	}
	return s
}

func stripTaskTrackingPolicy(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimSuffix(s, config.TaskTrackingPolicy))
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := writeFileRaw(dir, name, body); err != nil {
		t.Fatal(err)
	}
}

func TestRememberPermissionRuleUsesWorkspaceRoot(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cwd := robustTempDir(t)
	workspace := robustTempDir(t)
	t.Chdir(cwd)
	writeFile(t, cwd, "orca.toml", `
[permissions]
allow = ["Bash(cwd*)"]
`)
	writeFile(t, workspace, "orca.toml", `
[permissions]
allow = ["Bash(workspace*)"]
`)

	const rule = "Bash(go test ./...)"
	rememberPermissionRule(workspace, rule)

	cwdCfg := config.LoadForEdit(filepath.Join(cwd, "orca.toml"))
	if hasPermissionRule(cwdCfg.Permissions.Allow, rule) {
		t.Fatalf("remembered rule was written to cwd config: %v", cwdCfg.Permissions.Allow)
	}
	workspaceCfg := config.LoadForEdit(filepath.Join(workspace, "orca.toml"))
	if !hasPermissionRule(workspaceCfg.Permissions.Allow, rule) {
		t.Fatalf("remembered rule missing from workspace config: %v", workspaceCfg.Permissions.Allow)
	}
}

func TestRememberPermissionRuleCreatesWorkspaceConfigOverUserConfig(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	workspace := robustTempDir(t)
	userConfig := config.UserConfigPath()
	writeFile(t, filepath.Dir(userConfig), filepath.Base(userConfig), `
[permissions]
allow = ["Bash(user)"]
`)

	const rule = "Edit(src/app.go)"
	res := rememberPermissionRule(workspace, rule)
	if !res.Saved || res.Path != filepath.Join(workspace, "orca.toml") {
		t.Fatalf("remember result = %+v, want saved to workspace config", res)
	}

	userCfg := config.LoadForEdit(userConfig)
	if hasPermissionRule(userCfg.Permissions.Allow, rule) {
		t.Fatalf("workspace rule was written to user config: %v", userCfg.Permissions.Allow)
	}
	workspaceCfg := config.LoadForEdit(filepath.Join(workspace, "orca.toml"))
	if !hasPermissionRule(workspaceCfg.Permissions.Allow, rule) {
		t.Fatalf("workspace rule missing from project config: %v", workspaceCfg.Permissions.Allow)
	}
}

func TestRememberPermissionRuleEmptyRootUsesSourcePath(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cwd := robustTempDir(t)
	t.Chdir(cwd)
	userConfig := config.UserConfigPath()
	writeFile(t, filepath.Dir(userConfig), filepath.Base(userConfig), `
[permissions]
allow = ["Bash(user*)"]
`)

	const rule = "Bash(go env)"
	res := rememberPermissionRule("", rule)
	if !res.Saved || res.Path != userConfig {
		t.Fatalf("remember result = %+v, want saved to user source config", res)
	}

	userCfg := config.LoadForEdit(userConfig)
	if !hasPermissionRule(userCfg.Permissions.Allow, rule) {
		t.Fatalf("empty root should remember into SourcePath config: %v", userCfg.Permissions.Allow)
	}
	if _, err := os.Stat(filepath.Join(cwd, "orca.toml")); !os.IsNotExist(err) {
		t.Fatalf("empty root should not create cwd config when SourcePath exists, err=%v", err)
	}
}

func TestRememberPermissionRuleSkipsRuleCoveredByExistingAllow(t *testing.T) {
	workspace := robustTempDir(t)
	writeFile(t, workspace, "orca.toml", `
[permissions]
allow = ["Bash(go test:*)"]
`)

	res := rememberPermissionRule(workspace, "Bash(go test ./...)")
	if res.Saved || res.CoveredBy != "Bash(go test:*)" {
		t.Fatalf("remember result = %+v, want already covered", res)
	}
	cfg := config.LoadForEdit(filepath.Join(workspace, "orca.toml"))
	if len(cfg.Permissions.Allow) != 1 || cfg.Permissions.Allow[0] != "Bash(go test:*)" {
		t.Fatalf("allow rules = %v, want only existing prefix", cfg.Permissions.Allow)
	}
}

func TestRememberPermissionRulePrunesNarrowRulesWhenSavingBroaderRule(t *testing.T) {
	workspace := robustTempDir(t)
	writeFile(t, workspace, "orca.toml", `
[permissions]
allow = ["Bash(go test ./...)", "Bash(go build ./...)"]
`)

	res := rememberPermissionRule(workspace, "Bash(go test:*)")
	if !res.Saved || res.CoveredBy != "" {
		t.Fatalf("remember result = %+v, want saved broader rule", res)
	}
	cfg := config.LoadForEdit(filepath.Join(workspace, "orca.toml"))
	if hasPermissionRule(cfg.Permissions.Allow, "Bash(go test ./...)") {
		t.Fatalf("narrow go test rule should be pruned: %v", cfg.Permissions.Allow)
	}
	if !hasPermissionRule(cfg.Permissions.Allow, "Bash(go build ./...)") || !hasPermissionRule(cfg.Permissions.Allow, "Bash(go test:*)") {
		t.Fatalf("allow rules = %v, want unrelated exact plus prefix", cfg.Permissions.Allow)
	}
}

func hasPermissionRule(rules []string, want string) bool {
	for _, rule := range rules {
		if rule == want {
			return true
		}
	}
	return false
}

// TestBuildMigratesLegacyConfigEndToEnd drives the real boot path: a v0.x
// ~/.orca/config.json with no v1+ config present must be imported during
// Build — config written, scoped key resolution retained, and the user told via
// a notice.
func TestBuildMigratesLegacyConfigEndToEnd(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)                               // os.UserHomeDir on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")) // os.UserConfigDir on Linux
	t.Setenv("AppData", filepath.Join(home, "AppData"))         // os.UserConfigDir on Windows
	t.Setenv("DEEPSEEK_API_KEY", "")                            // keep host env empty; config scope must resolve the migrated file

	proj := robustTempDir(t)
	t.Chdir(proj)
	// codegraph off keeps Build offline; it merges over the migrated user config
	// without dropping the migrated plugins.
	writeFile(t, proj, "deepseek-orca.toml", "[codegraph]\nenabled = false\n")
	writeFile(t, filepath.Join(home, ".deepseek-orca"), "config.json",
		`{"apiKey":"sk-e2e","lang":"zh","mcpServers":{"fs":{"command":"npx","args":["-y","server-fs"]}}}`)
	writeFile(t, filepath.Join(home, ".deepseek-orca", "sessions"), "chat-1.events.jsonl",
		`{"type":"user.message","id":1,"ts":"t","turn":0,"text":"hello from v0.x"}`+"\n"+
			`{"type":"model.final","id":2,"ts":"t","turn":0,"content":"hi","toolCalls":[],"usage":{},"costUsd":0}`+"\n")

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	migrated := false
	for _, n := range notices {
		if strings.Contains(n, "migrated your previous configuration") {
			migrated = true
		}
	}
	if !migrated {
		t.Fatalf("no migration notice emitted; got %v", notices)
	}

	dest := config.UserConfigPath()
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("v2 config not written to %s: %v", dest, err)
	}
	if !strings.Contains(string(data), `name    = "fs"`) || !strings.Contains(string(data), `language      = "zh"`) {
		t.Errorf("migrated config missing plugin/lang:\n%s", data)
	}

	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "" {
		t.Errorf("migration must not mutate process environment: %q", got)
	}

	if data, err := os.ReadFile(config.UserCredentialsPath()); err != nil || !strings.Contains(string(data), "DEEPSEEK_API_KEY=sk-e2e") {
		t.Errorf("credentials store missing migrated key: %q (err %v)", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".env")); !os.IsNotExist(err) {
		t.Errorf("migration must not write the user's ~/.env, stat err=%v", err)
	}

	sessionImported := false
	for _, n := range notices {
		if strings.Contains(n, "imported") && strings.Contains(n, "past session") {
			sessionImported = true
		}
	}
	if !sessionImported {
		t.Errorf("no session-import notice emitted; got %v", notices)
	}
	migratedSession := filepath.Join(config.SessionDir(), "chat-1.jsonl")
	if _, err := os.Stat(migratedSession); err != nil {
		t.Errorf("legacy session not imported to %s: %v", migratedSession, err)
	}
}

func TestBuildMigratesLegacySessionsFromConfigSessionDir(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	proj := robustTempDir(t)
	writeFile(t, proj, "deepseek-orca.toml", "[codegraph]\nenabled = false\n")

	legacyDir := config.SessionDir()
	writeFile(t, legacyDir, "custom-root.events.jsonl",
		`{"type":"user.message","id":1,"ts":"t","turn":0,"text":"hello from redirected config root"}`+"\n"+
			`{"type":"model.final","id":2,"ts":"t","turn":0,"content":"hi from redirected root","toolCalls":[],"usage":{},"costUsd":0}`+"\n")

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	// Pass the project root via WorkspaceRoot instead of t.Chdir: changing the
	// process cwd into a t.TempDir makes Windows refuse to remove that dir during
	// test cleanup (the cwd counts as "in use"), which is the only thing this test
	// failed on. WorkspaceRoot loads the same config without touching the cwd.
	ctrl, err := Build(context.Background(), Options{Sink: sink, WorkspaceRoot: proj})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sessionPath := filepath.Join(config.SessionDir(), "custom-root.jsonl")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("legacy config-root session not imported to %s: %v", sessionPath, err)
	}
	if !strings.Contains(string(data), "hello from redirected config root") {
		t.Fatalf("migrated session missing legacy content:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(config.SessionDir(), ".legacy-imported.v0-events-config")); err != nil {
		t.Fatalf("config-root legacy import marker missing: %v", err)
	}
	sessionImported := false
	for _, n := range notices {
		if strings.Contains(n, "imported") && strings.Contains(n, "past session") && strings.Contains(n, legacyDir) {
			sessionImported = true
		}
	}
	if !sessionImported {
		t.Errorf("no config-root session-import notice emitted; got %v", notices)
	}
}

// isolateConfigHome redirects os.UserConfigDir() (and the cache subtree under
// it) at a per-test temp dir by overriding the env vars Go's stdlib reads —
// HOME on darwin, XDG_CONFIG_HOME on linux. Without this, Build's plugin path
// would persist startup stats and cached schemas into the developer's real
// ~/Library/Application Support tree and bleed state across tests. Mirrors the
// withTempCache helper in internal/plugin/stats_test.go.
func isolateConfigHome(t *testing.T) string {
	t.Helper()
	dir := robustTempDir(t)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "AppData", "Local"))
	t.Setenv("USERPROFILE", dir)
	return dir
}

// TestPartitionByTier pins the bucket assignment contract that the rest of
// boot.go's plugin orchestration depends on: each tier string maps to its own
// slice, the original order inside a tier is preserved (so /mcp status and
// stats land deterministically), an empty/missing tier defaults to background
// (connects after session start without blocking chat), and unknown non-empty
// values fall back to lazy so a typo never forces unwanted background connects.
func TestPartitionByTier(t *testing.T) {
	entries := []config.PluginEntry{
		{Name: "e1", Tier: "eager"},
		{Name: "l1", Tier: "lazy"},
		{Name: "b1", Tier: "background"},
		{Name: "default", Tier: ""}, // empty defaults to background
	}

	eager, lazy, bg := partitionByTier(entries)

	if len(eager) != 1 || eager[0].Name != "e1" {
		t.Fatalf("eager bucket = %+v, want [e1]", eager)
	}
	if len(bg) != 2 || bg[0].Name != "b1" || bg[1].Name != "default" {
		t.Fatalf("background bucket = %+v, want [b1, default] preserving input order", bg)
	}
	if len(lazy) != 1 || lazy[0].Name != "l1" {
		t.Fatalf("lazy bucket = %+v, want [l1]", lazy)
	}
}

func TestBuildMigratesLegacyEagerTierToBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"

[[plugins]]
name = "legacy-eager"
command = "deepseek-orca-missing-legacy-eager-mcp"
tier = "eager"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "legacy-eager", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "legacy-eager" {
		t.Fatalf("failures = %+v, want background startup failure for migrated legacy eager plugin", failures)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "deepseek-orca.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\ntier") {
		t.Fatalf("legacy eager tier should be removed during load:\n%s", raw)
	}
}

func TestBuildMigratesLegacyLazyTierToBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"

[[plugins]]
name = "legacy-lazy"
command = "deepseek-orca-missing-legacy-lazy-mcp"
tier = "lazy"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "legacy-lazy", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "legacy-lazy" {
		t.Fatalf("failures = %+v, want background startup failure for migrated legacy lazy plugin", failures)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "deepseek-orca.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\ntier") {
		t.Fatalf("legacy lazy tier should be removed during load:\n%s", raw)
	}
}

func TestBuildColdCodegraphStartsInBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	launcher := writeCodegraphHelper(t, dir)
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	writeFile(t, dir, "deepseek-orca.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = true
path = %q
tier = "background"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`, launcher))

	var notices []event.Event
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if got := ctrl.Host().Failures(); len(got) != 0 {
		t.Fatalf("Host.Failures() = %+v, want empty for cold built-in codegraph background startup", got)
	}
	codegraphDir := filepath.Join(dir, ".codegraph")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(codegraphDir); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("cold codegraph init did not create .codegraph/: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	foundNotice := false
	for _, n := range notices {
		if strings.Contains(n.Text, "preparing code-intelligence tools in the background") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Fatalf("missing background warmup notice; got %+v", notices)
	}
}

func TestBuildWarmCodegraphIgnoresLegacyEagerTier(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake launcher is a POSIX-sh script")
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "slow-codegraph")
	writeFile(t, dir, "slow-codegraph", "#!/bin/sh\nif [ \"$1\" = serve ]; then sleep 5; fi\n")
	if err := os.Chmod(launcher, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "deepseek-orca.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = true
path = %q
tier = "eager"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`, launcher))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build should not block on warm codegraph with legacy eager tier: %v", err)
	}
	defer ctrl.Close()
}

func TestBuildDefaultsToNearestGitRoot(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "cmd", "tool")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "deepseek-orca.toml", `
default_model = "root-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "root-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"
`)
	t.Chdir(subdir)

	ctrl, err := Build(context.Background(), Options{Model: "root-model"})
	if err != nil {
		t.Fatalf("Build should load config from nearest git root: %v", err)
	}
	defer ctrl.Close()
}

func TestBuildMigratesLegacyEagerBeforeStatsDemotion(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	// Three samples above 2*budget — the rule in stats.go's Recommend triggers
	// when the trailing window is entirely over the threshold. Use 30s so even
	// future budget bumps stay below the threshold.
	for i := 0; i < 3; i++ {
		if err := plugin.RecordStartup("slowserver", 30*time.Second); err != nil {
			t.Fatalf("RecordStartup #%d: %v", i, err)
		}
	}

	writeFile(t, dir, "deepseek-orca.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "DEEPSEEK_ORCA_TEST_KEY_UNSET"

[[plugins]]
name = "slowserver"
command = "deepseek-orca-missing-slow-mcp-binary"
tier = "eager"
`)

	var notices []event.Event
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "slowserver", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "slowserver" {
		t.Fatalf("Host.Failures() = %+v, want background startup failure for migrated plugin", failures)
	}

	foundDemoteNotice := false
	for _, n := range notices {
		if strings.Contains(n.Text, "demoting to lazy") {
			foundDemoteNotice = true
			break
		}
	}
	if foundDemoteNotice {
		t.Fatalf("legacy tier should be migrated before demotion logic; got notices %+v", notices)
	}
}

func waitForMCPFailure(t *testing.T, h *plugin.Host, name string, timeout time.Duration) []plugin.Failure {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		failures := h.Failures()
		for _, f := range failures {
			if f.Name == name {
				return failures
			}
		}
		if time.Now().After(deadline) {
			return failures
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHelperProcess is invoked as a subprocess by TestBuildEagerStartsAtBoot
// and TestBuildLazyDoesNotConnectAtBoot. It mirrors the minimal MCP stdio
// server in internal/plugin/plugin_test.go so the boot package can drive an
// end-to-end handshake without depending on the plugin package's test helper
// (Go's testing framework only re-invokes the binary of the test package
// currently running). The helper gates on GO_WANT_HELPER_PROCESS=1 so a
// normal `go test ./internal/boot/...` does not trip it.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // notification: no response
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "mock", "version": "0"},
				"capabilities":    map[string]any{},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "echo",
				"description": "Echo back the message.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"msg": map[string]any{"type": "string"}},
					"required":   []string{"msg"},
				},
			}}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		os.Stdout.Write(append(b, '\n'))
	}
}

func writeCodegraphHelper(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "codegraph-helper")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	src := filepath.Join(dir, "codegraph-helper.go")
	if err := os.WriteFile(src, []byte(`package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "init" {
		_ = os.MkdirAll(filepath.Join(os.Args[2], ".codegraph"), 0o755)
		return
	}

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var req struct {
			ID     *int            `+"`json:\"id\"`"+`
			Method string          `+"`json:\"method\"`"+`
			Params json.RawMessage `+"`json:\"params\"`"+`
		}
		if err := json.Unmarshal(line, &req); err != nil || req.ID == nil {
			continue
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "codegraph", "version": "0"},
				"capabilities":    map[string]any{},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "search",
				"description": "Search symbols.",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", path, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build codegraph helper: %v\n%s", err, out)
	}
	return path
}
