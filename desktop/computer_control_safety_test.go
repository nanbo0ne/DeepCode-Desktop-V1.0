package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type controlTestProvider struct {
	stream func(context.Context, provider.Request) (<-chan provider.Chunk, error)
}

func computerParentTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := agent.WithParentTurn(agent.WithParentSession(context.Background(), "test-parent-session"))
	t.Cleanup(cancel)
	return ctx
}

func (*controlTestProvider) Name() string { return "fake-control" }
func (p *controlTestProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	return p.stream(ctx, r)
}
func controlChunks(chunks ...provider.Chunk) <-chan provider.Chunk {
	ch := make(chan provider.Chunk, len(chunks)+1)
	for _, v := range chunks {
		ch <- v
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch
}
func controlCall(name, args string) <-chan provider.Chunk {
	return controlChunks(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "test-call", Name: name, Arguments: args}})
}

type loopTestBackend struct {
	actions int
	fail    bool
}

func (*loopTestBackend) Capabilities() computeruse.Capabilities {
	return computeruse.Capabilities{Supported: true}
}
func (*loopTestBackend) Observe(_ context.Context, id string, g uint64) (computeruse.Observation, error) {
	return computeruse.Observation{SessionID: id, Generation: g, Screenshot: fmt.Sprintf("synthetic-%d", g), ScreenshotMIME: "image/png", Summary: "synthetic screen"}, nil
}
func (b *loopTestBackend) Execute(ctx context.Context, _ computeruse.Observation, _ computeruse.Action) error {
	b.actions++
	if b.fail {
		return errors.New("uncertain input")
	}
	return ctx.Err()
}
func (*loopTestBackend) StartSafetyHooks(func(), func()) error      { return nil }
func (*loopTestBackend) StopSafetyHooks()                           {}
func (*loopTestBackend) ReleaseInjectedInput()                      {}
func (*loopTestBackend) ShowOverlay(computeruse.OverlayState) error { return nil }
func (*loopTestBackend) UpdateOverlay(computeruse.OverlayState)     {}
func (*loopTestBackend) HideOverlay()                               {}

func TestComputerLoopBoundsImagesAndReobservesCompletion(t *testing.T) {
	b := &loopTestBackend{}
	a := &App{computerUse: computeruse.NewService(b, nil)}
	lastImage := ""
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if !r.DisableThinking {
			t.Fatal("thinking enabled")
		}
		count := 0
		current := ""
		for _, m := range r.Messages {
			for _, i := range m.Images {
				count++
				current = i.Data
			}
		}
		if count != 1 {
			t.Fatalf("images=%d", count)
		}
		if len(r.Tools) == 0 {
			if current == lastImage {
				t.Fatal("completion reused old screenshot")
			}
			return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":true,"reason":"synthetic result visible"}`}), nil
		}
		if len(r.Messages) != 3 || len(r.Messages[1].Content) > 26000 {
			t.Fatal("unbounded context")
		}
		lastImage = current
		if b.actions < 18 {
			return controlCall("computer_action", `{"type":"wait","timeoutMs":1}`), nil
		}
		return controlCall("computer_complete", `{"summary":"done"}`), nil
	}}
	result, err := a.runComputerTaskWithProvider(computerParentTestContext(t), computeruse.StartRequest{TabID: "a", Goal: "test"}, p)
	if err != nil || result != "done" || a.computerUse.Current().State != computeruse.StateSucceeded {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestComputerLoopRejectsFalseOrStaleCompletion(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			a := &App{computerUse: computeruse.NewService(&loopTestBackend{}, nil)}
			p := &controlTestProvider{stream: func(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
				if len(r.Tools) > 0 {
					return controlCall("computer_complete", `{"summary":"claimed"}`), nil
				}
				if stale {
					if _, err := a.computerUse.Observe(ctx); err != nil {
						t.Fatal(err)
					}
				}
				return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: fmt.Sprintf(`{"satisfied":%t,"reason":"test evidence"}`, stale)}), nil
			}}
			ctx := computerParentTestContext(t)
			if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "test"}, p); err == nil {
				t.Fatal("false/stale completion accepted")
			}
			if a.computerUse.Current().State == computeruse.StateSucceeded {
				t.Fatal("false completion recorded")
			}
			if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "retry"}, p); !errors.Is(err, computeruse.ErrParentTurnStopped) {
				t.Fatalf("false completion allowed replay: %v", err)
			}
		})
	}
}

func TestComputerLoopNeverRetriesUncertainAction(t *testing.T) {
	b := &loopTestBackend{fail: true}
	a := &App{computerUse: computeruse.NewService(b, nil)}
	calls := 0
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		calls++
		return controlCall("computer_action", `{"type":"type_text","text":"synthetic"}`), nil
	}}
	ctx := computerParentTestContext(t)
	if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "test"}, p); err == nil {
		t.Fatal("uncertain action accepted")
	}
	if _, err := a.runComputerTask(ctx, computeruse.StartRequest{TabID: "a", Goal: "new goal"}); !errors.Is(err, computeruse.ErrParentTurnStopped) {
		t.Fatalf("production entry allowed uncertain replay: %v", err)
	}
	if calls != 1 || b.actions != 1 {
		t.Fatal("input replayed")
	}
}

func TestComputerLoopEscalationResumesSameTask(t *testing.T) {
	a := &App{computerUse: computeruse.NewService(&loopTestBackend{}, nil)}
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		return controlCall("computer_escalate", `{"reason":"choose next step"}`), nil
	}}
	parent, cancel := context.WithCancel(computerParentTestContext(t))
	_, err := a.runComputerTaskWithProvider(parent, computeruse.StartRequest{TabID: "a", Goal: "test", Restrictions: "no send"}, p)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	id := a.computerUse.Current().ID
	if _, err := a.runComputerTaskWithProvider(computerParentTestContext(t), computeruse.StartRequest{TabID: "b", ResumeSessionID: id}, p); !errors.Is(err, computeruse.ErrWrongOwner) {
		t.Fatal(err)
	}
	if _, err := a.runComputerTaskWithProvider(computerParentTestContext(t), computeruse.StartRequest{TabID: "a", ResumeSessionID: id, Restrictions: "allow send"}, p); err == nil {
		t.Fatal("scope changed")
	}
	if _, err := a.runComputerTaskWithProvider(computerParentTestContext(t), computeruse.StartRequest{TabID: "a", ResumeSessionID: id, Guidance: "choose visible option"}, p); err != nil {
		t.Fatal(err)
	}
	if a.computerUse.Current().ID != id || a.computerUse.Current().Restrictions != "no send" {
		t.Fatal("resume changed task")
	}
	_ = a.computerUse.Stop("cleanup")
}

func TestComputerModelInvalidExplicitRolesNeverFallback(t *testing.T) {
	cfg := &config.Config{Providers: []config.ProviderEntry{{Name: "official", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash-vision-exp"}}}
	a := &App{}
	if ref, err := a.resolveComputerControlModel(cfg, ""); err != nil || ref != "official/deepseek-v4-flash-vision-exp" {
		t.Fatalf("ref=%q err=%v", ref, err)
	}
	if _, err := a.resolveComputerControlModel(cfg, "invalid/requested"); err == nil {
		t.Fatal("invalid requested model fell back")
	}
	cfg.Desktop.ComputerControlModel = "invalid/computer"
	if _, err := a.resolveComputerControlModel(cfg, ""); err == nil {
		t.Fatal("invalid role fell back")
	}
	cfg.Desktop.ComputerControlModel = ""
	cfg.Agent.SubagentModels = map[string]string{"vision": "invalid/vision"}
	if _, err := a.resolveComputerControlModel(cfg, ""); err == nil {
		t.Fatal("invalid vision role fell back")
	}
}

func TestComputerConfigUsesOwningTabNotActiveTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	for root, model := range map[string]string{rootA: "owner/model", rootB: "active/model"} {
		path := projectConfigPathForRoot(root)
		cfg := config.LoadForEdit(path)
		cfg.Agent.SubagentModels = map[string]string{"vision": model}
		if err := cfg.SaveTo(path); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{activeTabID: "b", tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: rootA}, "b": {ID: "b", WorkspaceRoot: rootB}}}
	cfg, err := a.computerTaskConfig("a")
	if err != nil || cfg.Agent.SubagentModels["vision"] != "owner/model" {
		t.Fatalf("wrong owning config: %v", err)
	}
	for _, id := range []string{"", "missing"} {
		if _, err := a.computerTaskConfig(id); err == nil {
			t.Fatalf("invalid tab %q fell back to active", id)
		}
	}
}

func TestComputerReceiptBound(t *testing.T) {
	var receipts []string
	for i := 0; i < 100; i++ {
		receipts = appendComputerReceipt(receipts, strings.Repeat("x", 5000))
	}
	if len(receipts) != 12 || len(receipts[0]) > 2070 {
		t.Fatal("receipts not bounded")
	}
	count := 0
	for _, m := range computerControlMessages(computeruse.StartRequest{}, computeruse.Observation{Screenshot: "synthetic"}, 0, receipts) {
		count += len(m.Images)
	}
	if count != 1 {
		t.Fatal("screenshot duplicated")
	}
}

func TestComputerStopCancelsProviderWithoutInput(t *testing.T) {
	b := &loopTestBackend{}
	a := &App{computerUse: computeruse.NewService(b, nil)}
	entered := make(chan context.Context, 1)
	p := &controlTestProvider{stream: func(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
		entered <- ctx
		return make(chan provider.Chunk), nil
	}}
	done := make(chan error, 1)
	parent := computerParentTestContext(t)
	go func() {
		_, err := a.runComputerTaskWithProvider(parent, computeruse.StartRequest{TabID: "a", Goal: "test"}, p)
		done <- err
	}()
	providerCtx := <-entered
	if _, err := (computerStopTool{app: a, tabID: "b"}).Execute(context.Background(), nil); !errors.Is(err, computeruse.ErrWrongOwner) {
		t.Fatalf("other tab stopped owner: %v", err)
	}
	if err := a.StopComputerUse(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider did not cancel")
	}
	if providerCtx.Err() == nil || b.actions != 0 || a.computerUse.Current().State != computeruse.StateCancelled {
		t.Fatal("stop did not cancel provider before input")
	}
}
