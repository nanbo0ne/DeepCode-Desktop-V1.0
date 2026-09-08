package agent

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func testTaskContext() context.Context {
	return WithParentSession(context.Background(), "parent-session")
}

// TestTaskToolReturnsSubAgentFinalAnswer runs a task against a mock provider
// that emits a single text turn, and verifies the tool returns that text with a
// transcript reference — sub-agent intermediate state isn't supposed to leak.
func TestTaskToolReturnsSubAgentFinalAnswer(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "found 3 callers of Foo"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	task := newTestTaskTool(t, sub, parentReg, "test-sys-prompt", "", "", nil)

	out, err := task.Execute(testTaskContext(), []byte(`{"prompt":"find callers of Foo"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_ = subagentRefFromOutput(t, out)
	if !strings.Contains(out, "found 3 callers of Foo") {
		t.Errorf("got %q, want sub-agent final answer", out)
	}

	// The sub-agent must have received the prompt as its user message and
	// the configured system prompt at the top — proving the session was
	// fresh, not the parent's.
	if sys := sub.lastReq.Messages[0]; sys.Role != provider.RoleSystem || sys.Content != "test-sys-prompt" {
		t.Errorf("first message = %+v, want system 'test-sys-prompt'", sys)
	}
	if got := lastUser(sub.lastReq); got != "find callers of Foo" {
		t.Errorf("sub-agent user = %q, want the prompt verbatim", got)
	}
}

func TestTaskToolRoutesValidatedImagesToSupportedSubagent(t *testing.T) {
	sub := &mockProvider{name: "vision-sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "the image contains a chart"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).
		WithVision("auto", func(modelRef string) string {
			if modelRef != "base-model" {
				t.Fatalf("capability checked for %q, want base-model", modelRef)
			}
			return "supported"
		}, func(_ context.Context, image provider.ImageContent) (provider.ImageContent, error) {
			image.Data = "HYDRATED_IMAGE_DATA"
			return image, nil
		}).WithVisionDefault("base-model")
	ctx := WithTurnImages(testTaskContext(), []provider.ImageContent{{
		Name:      "chart.png",
		Path:      ".orca/attachments/chart.png",
		MediaType: "image/png",
	}})
	if _, err := task.Execute(ctx, []byte(`{"prompt":"inspect the chart","images":["chart.png"]}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	user := sub.lastReq.Messages[len(sub.lastReq.Messages)-1]
	if len(user.Images) != 1 || user.Images[0].Data != "HYDRATED_IMAGE_DATA" {
		t.Fatalf("subagent images = %+v", user.Images)
	}
}

func TestTaskToolVisionDefaultOnlyAppliesToImageTasks(t *testing.T) {
	sub := &mockProvider{name: "vision-sub", streams: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "general"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "vision"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "explicit"}, {Type: provider.ChunkDone}},
	}}
	var models []string
	resolve := func(model, _ string) (provider.Provider, *provider.Pricing, int, error) {
		models = append(models, model)
		return sub, nil, 0, nil
	}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "general-model", "", resolve).
		WithVisionDefault("vision-model").
		WithVision("on", nil, func(_ context.Context, image provider.ImageContent) (provider.ImageContent, error) {
			image.Data = "hydrated"
			return image, nil
		})
	ctx := WithTurnImages(testTaskContext(), []provider.ImageContent{{Name: "chart.png", Path: "chart.png", MediaType: "image/png"}})

	for _, args := range []string{
		`{"prompt":"ordinary task"}`,
		`{"prompt":"inspect chart","images":["chart.png"]}`,
		`{"prompt":"inspect chart explicitly","model":"explicit-model","images":["chart.png"]}`,
	} {
		if _, err := task.Execute(ctx, []byte(args)); err != nil {
			t.Fatalf("Execute(%s): %v", args, err)
		}
	}
	if got, want := strings.Join(models, ","), "general-model,vision-model,explicit-model"; got != want {
		t.Fatalf("resolved models = %q, want %q", got, want)
	}
}

func TestTaskToolRejectsImplicitImageRoutingWithoutVisionDefault(t *testing.T) {
	sub := &mockProvider{name: "qwen-general", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "should not run"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "qwen-general", "", func(model, _ string) (provider.Provider, *provider.Pricing, int, error) {
		return sub, nil, 0, nil
	}).WithVision("auto", func(string) string { return "supported" }, func(_ context.Context, image provider.ImageContent) (provider.ImageContent, error) {
		image.Data = "hydrated"
		return image, nil
	})
	ctx := WithTurnImages(testTaskContext(), []provider.ImageContent{{Name: "chart.png", Path: "chart.png", MediaType: "image/png"}})

	args := []byte(`{"prompt":"inspect chart","images":["chart.png"]}`)
	if profile := task.ResolveProfile(args); profile != nil {
		t.Fatalf("profile = %+v, want nil until Execute reports missing vision configuration", profile)
	}
	_, err := task.Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "configured vision model") {
		t.Fatalf("Execute error = %v, want configured-vision-model error", err)
	}
	if len(sub.requests) != 0 {
		t.Fatalf("implicit image routing invoked general provider %d time(s)", len(sub.requests))
	}
}

func TestTaskToolAllowsExplicitImageModelWithoutVisionDefault(t *testing.T) {
	sub := &mockProvider{name: "explicit-vision", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "explicit"}, {Type: provider.ChunkDone},
	}}
	var gotModel string
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "qwen-general", "", func(model, _ string) (provider.Provider, *provider.Pricing, int, error) {
		gotModel = model
		return sub, nil, 0, nil
	}).WithVision("on", nil, func(_ context.Context, image provider.ImageContent) (provider.ImageContent, error) {
		image.Data = "hydrated"
		return image, nil
	})
	ctx := WithTurnImages(testTaskContext(), []provider.ImageContent{{Name: "chart.png", Path: "chart.png", MediaType: "image/png"}})

	if _, err := task.Execute(ctx, []byte(`{"prompt":"inspect chart","model":"explicit-vision","images":["chart.png"]}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotModel != "explicit-vision" {
		t.Fatalf("resolved model = %q, want explicit model", gotModel)
	}
}

func TestTaskToolRejectsDisallowedImageRouting(t *testing.T) {
	available := []provider.ImageContent{{Name: "allowed.png", Path: ".orca/attachments/allowed.png", MediaType: "image/png"}}
	ctx := WithTurnImages(testTaskContext(), available)
	newTask := func(mode, capability string) *TaskTool {
		return newTestTaskTool(t, &mockProvider{name: "sub"}, tool.NewRegistry(), "sys", "", "", nil).
			WithVisionDefault("base-model").WithVision(mode, func(string) string { return capability }, func(_ context.Context, image provider.ImageContent) (provider.ImageContent, error) {
			image.Data = "data"
			return image, nil
		})
	}
	for _, tc := range []struct {
		name       string
		mode       string
		capability string
		image      string
		want       string
	}{
		{name: "off", mode: "off", capability: "supported", image: "allowed.png", want: "disabled"},
		{name: "auto unknown", mode: "auto", capability: "unknown", image: "allowed.png", want: "not confirmed"},
		{name: "auto unsupported", mode: "auto", capability: "unsupported", image: "allowed.png", want: "not confirmed"},
		{name: "arbitrary path", mode: "on", capability: "supported", image: `C:\\secrets\\private.png`, want: "validated attachments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTask(tc.mode, tc.capability).Execute(ctx, []byte(`{"prompt":"inspect","images":[`+strconv.Quote(tc.image)+`]}`))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Execute error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// TestTaskToolFiltersTools verifies the whitelist behaviour: when the caller
// names a subset of tools, the sub-agent's registry contains exactly that set
// with subagent/skill meta-tools stripped to prevent recursive delegation.
func TestTaskToolFiltersTools(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "read_file", readOnly: true})
	parentReg.Add(fakeTool{name: "write_file", readOnly: false})
	parentReg.Add(fakeTool{name: "bash", readOnly: false})
	task := newTestTaskTool(t, sub, parentReg, "sys", "", "", nil)
	parentReg.Add(task) // simulate the wiring in cli.setup
	parentReg.Add(fakeTool{name: "run_skill", readOnly: false})
	parentReg.Add(fakeTool{name: "research", readOnly: false})

	args := []byte(`{"prompt":"x","tools":["read_file","task","write_file","run_skill","research"]}`)
	if _, err := task.Execute(testTaskContext(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The sub-agent's tool schemas should reflect the whitelist minus meta-tools.
	got := map[string]bool{}
	for _, s := range sub.lastReq.Tools {
		got[s.Name] = true
	}
	if !got["read_file"] || !got["write_file"] || got["task"] || got["run_skill"] || got["research"] || got["bash"] {
		t.Errorf("sub-agent tools = %v, want {read_file, write_file} (meta-tools stripped, bash not requested)", got)
	}
}

// TestTaskToolDefaultsToParentToolsWithoutMetaTools covers the no-whitelist
// path: the sub-agent inherits parent tools except subagent/skill meta-tools.
func TestTaskToolDefaultsToParentToolsWithoutMetaTools(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "read_file", readOnly: true})
	parentReg.Add(fakeTool{name: "grep", readOnly: true})
	task := newTestTaskTool(t, sub, parentReg, "sys", "", "", nil)
	parentReg.Add(task)
	parentReg.Add(fakeTool{name: "run_skill", readOnly: false})
	parentReg.Add(fakeTool{name: "explore", readOnly: false})
	parentReg.Add(fakeTool{name: "research", readOnly: false})
	parentReg.Add(fakeTool{name: "review", readOnly: false})
	parentReg.Add(fakeTool{name: "security_review", readOnly: false})
	parentReg.Add(fakeTool{name: "remember", readOnly: false})

	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sub.lastReq.Tools {
		got[s.Name] = true
	}
	if !got["read_file"] || !got["grep"] || !got["remember"] ||
		got["task"] || got["run_skill"] || got["explore"] || got["research"] || got["review"] || got["security_review"] {
		t.Errorf("default sub-agent tools = %v, want normal tools inherited and meta-tools stripped", got)
	}
}

func TestTaskToolUsesConfiguredProfileForExecution(t *testing.T) {
	parent := &mockProvider{name: "parent", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "parent answer"},
		{Type: provider.ChunkDone},
	}}
	resolved := &mockProvider{name: "resolved", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "resolved answer"},
		{Type: provider.ChunkDone},
	}}
	var gotModel, gotEffort string
	resolve := func(model, effort string) (provider.Provider, *provider.Pricing, int, error) {
		gotModel, gotEffort = model, effort
		return resolved, nil, 0, nil
	}
	task := newTestTaskTool(t, parent, tool.NewRegistry(), "sys", "deepseek-pro", "max", resolve)

	out, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "resolved answer") {
		t.Fatalf("sub-agent did not use resolved provider, got %q", out)
	}
	if gotModel != "deepseek-pro" || gotEffort != "max" {
		t.Fatalf("resolved profile = %q/%q, want deepseek-pro/max", gotModel, gotEffort)
	}
}

func TestTaskToolReturnsProfileResolutionErrors(t *testing.T) {
	parent := &mockProvider{name: "parent", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "parent answer"},
		{Type: provider.ChunkDone},
	}}
	resolve := func(string, string) (provider.Provider, *provider.Pricing, int, error) {
		return nil, nil, 0, errors.New("bad effort")
	}
	task := newTestTaskTool(t, parent, tool.NewRegistry(), "sys", "", "", resolve)

	_, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x","effort":"turbo"}`))
	if err == nil || !strings.Contains(err.Error(), "bad effort") {
		t.Fatalf("Execute error = %v, want profile resolution error", err)
	}
}

func TestTaskToolBackgroundGuidanceRequiresWaitForDependentWork(t *testing.T) {
	task := newTestTaskTool(t, &mockProvider{name: "sub"}, tool.NewRegistry(), "sys", "", "", nil)
	desc := task.Description()
	if !strings.Contains(desc, "call wait before giving the final answer") {
		t.Fatalf("Description() missing dependent-background wait guidance:\n%s", desc)
	}
	schema := string(task.Schema())
	for _, want := range []string{
		"Use only when the user explicitly wants background work",
		"If the current answer needs this research",
		"call wait first",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("Schema() missing %q:\n%s", want, schema)
		}
	}
}

func TestTaskToolRequiresTranscriptStore(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"},
		{Type: provider.ChunkDone},
	}}
	task := NewTaskTool(sub, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0.0, "", "sys", nil, "", "", nil)

	_, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "transcript store is required") {
		t.Fatalf("Execute error = %v, want transcript store requirement", err)
	}
}

// TestTaskToolRunsEphemerallyWithoutParentSession mirrors headless `deepseek-orca run`:
// the store is wired but the context carries no parent session, so the sub-agent
// must run without persistence and return its plain answer (no transcript ref).
func TestTaskToolRunsEphemerallyWithoutParentSession(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "headless answer"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil)

	out, err := task.Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "headless answer") {
		t.Fatalf("got %q, want sub-agent final answer", out)
	}
	if strings.Contains(out, "Subagent reference") {
		t.Fatalf("ephemeral run should not emit a transcript reference: %q", out)
	}
}

func TestTaskToolRejectsContinuationWithoutParentSession(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil)

	_, err := task.Execute(context.Background(), []byte(`{"prompt":"x","continue_from":"sa_whatever"}`))
	if err == nil || !strings.Contains(err.Error(), "persisted session") {
		t.Fatalf("Execute error = %v, want persisted-session requirement", err)
	}
}

func TestTaskToolPersistsAndContinuesTranscript(t *testing.T) {
	sub := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "first answer"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "second answer"},
			{Type: provider.ChunkDone},
		},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	store := NewSubagentStore(t.TempDir())
	task := newTestTaskTool(t, sub, reg, "sys", "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	first, err := task.Execute(testTaskContext(), []byte(`{"prompt":"first task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, first)
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.ParentSession != "parent-session" {
		t.Fatalf("parent session = %q, want parent-session", meta.ParentSession)
	}
	if !strings.Contains(first, "first answer") {
		t.Fatalf("first output = %q, want answer", first)
	}

	second, err := task.Execute(testTaskContext(), []byte(`{"prompt":"second task","continue_from":"`+ref+`"}`))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if !strings.Contains(second, "second answer") {
		t.Fatalf("second output = %q, want answer", second)
	}
	if len(sub.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(sub.requests))
	}
	msgs := sub.requests[1].Messages
	if len(msgs) < 4 {
		t.Fatalf("continued request messages = %+v, want prior transcript plus new task", msgs)
	}
	if msgs[1].Content != "first task" || msgs[2].Content != "first answer" || lastUser(sub.requests[1]) != "second task" {
		t.Fatalf("continued request messages = %+v, want first task/answer then second task", msgs)
	}
}

func TestTaskToolFailedForegroundContinuationPersistsAndRejectsReuse(t *testing.T) {
	sub := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "first answer"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkError, Err: errors.New("provider failed")},
		},
	}}
	store := NewSubagentStore(t.TempDir())
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	task := NewTaskTool(sub, nil, reg, 20, 0, 0, 0, 0, 0.0, "", "sys", nil, "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	first, err := task.Execute(testTaskContext(), []byte(`{"prompt":"first task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, first)

	_, err = task.Execute(testTaskContext(), []byte(`{"prompt":"second task","continue_from":"`+ref+`"}`))
	if err == nil || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("second Execute error = %v, want provider failure", err)
	}
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != SubagentFailed {
		t.Fatalf("status = %q, want failed", meta.Status)
	}
	loaded, err := LoadSession(store.sessionPath(ref))
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	msgs := loaded.Snapshot()
	if len(msgs) != 4 || msgs[1].Content != "first task" || msgs[2].Content != "first answer" || msgs[3].Content != "second task" {
		t.Fatalf("failed continuation transcript = %+v, want first task/answer plus second task", msgs)
	}
	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"third task","continue_from":"`+ref+`"}`)); err == nil || !strings.Contains(err.Error(), "failed and cannot be continued") {
		t.Fatalf("reuse error = %v, want failed ref rejection", err)
	}
}

func TestTaskToolRejectsMismatchedContinuationProfile(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).
		WithTranscripts(NewSubagentStore(t.TempDir()), t.TempDir(), "base-model", "")

	out, err := task.Execute(testTaskContext(), []byte(`{"prompt":"first task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, out)
	_, err = task.Execute(testTaskContext(), []byte(`{"prompt":"second task","continue_from":"`+ref+`","model":"other-model"}`))
	if err == nil || !strings.Contains(err.Error(), "model/effort") {
		t.Fatalf("mismatched model error = %v, want compatibility failure", err)
	}
}

func subagentRefFromOutput(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Subagent reference: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Subagent reference: "))
		}
	}
	t.Fatalf("no subagent reference in output:\n%s", out)
	return ""
}

func newTestTaskTool(t *testing.T, prov provider.Provider, reg *tool.Registry, sysPrompt, subagentModel, subagentEffort string, resolve func(string, string) (provider.Provider, *provider.Pricing, int, error)) *TaskTool {
	t.Helper()
	return NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, 0.0, "", sysPrompt, nil, subagentModel, subagentEffort, resolve).
		WithTranscripts(NewSubagentStore(t.TempDir()), t.TempDir(), "base-model", "base-effort")
}
