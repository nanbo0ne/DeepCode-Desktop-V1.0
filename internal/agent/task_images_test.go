package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestTaskImageModelPrecedence(t *testing.T) {
	task := &TaskTool{baseModel: "current", subagentModel: "general", visionMode: "auto", visionCapability: func(string) string { return "supported" }}
	task.WithVisionFallback("official/vision")
	for _, tc := range []struct{ explicit, role, capability, want string }{
		{"chosen", "role", "supported", "chosen"},
		{"", "role", "supported", "role"},
		{"", "", "supported", "current"},
		{"", "", "unknown", "official/vision"},
		{"", "", "unsupported", "official/vision"},
	} {
		task.WithVisionDefault(tc.role)
		task.visionCapability = func(string) string { return tc.capability }
		got, _ := task.effectiveProfileForImages(tc.explicit, "", true)
		if got != tc.want {
			t.Fatalf("%+v: got %q", tc, got)
		}
	}
	if got, _ := task.effectiveProfileForImages("", "", false); got != "general" {
		t.Fatalf("ordinary model = %q", got)
	}
}

func TestTaskImageResolverPinsBytesAcrossRuntimeTOCTOU(t *testing.T) {
	sub := &mockProvider{name: "vision", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "inspected"}, {Type: provider.ChunkDone}}}
	pinned := "approved bytes"
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", func(model, effort string) (provider.Provider, *provider.Pricing, int, error) {
		// Provider construction happens after image authorization.
		pinned = "replacement bytes"
		return sub, nil, 0, nil
	}).WithVision("auto", func(string) string { return "supported" }, func(context.Context, provider.ImageContent) (provider.ImageContent, error) {
		t.Fatal("task reopened the image after authorization")
		return provider.ImageContent{}, nil
	}).WithTranscriptIdentityResolver(func(model, effort string) (string, string) { return "selected-provider/vision", effort })
	var selectedModel string
	ctx := WithTaskImageResolver(testTaskContext(), func(_ context.Context, names []string, model string) ([]provider.ImageContent, error) {
		selectedModel = model
		return []provider.ImageContent{{Path: ".orca/attachments/pinned.png", Data: pinned, MediaType: "image/png"}}, nil
	})
	if _, err := task.Execute(ctx, []byte(`{"prompt":"inspect","model":"vision","images":["generated/frame.png"]}`)); err != nil {
		t.Fatal(err)
	}
	if selectedModel != "selected-provider/vision" || pinned != "replacement bytes" {
		t.Fatalf("identity/runtime: %q %q", selectedModel, pinned)
	}
	last := sub.lastReq.Messages[len(sub.lastReq.Messages)-1]
	if len(last.Images) != 1 || last.Images[0].Data != "approved bytes" {
		t.Fatalf("authorized bytes changed: %+v", last.Images)
	}
}

func TestTaskImageResolverDenyAndVisionOffNeverCallProvider(t *testing.T) {
	for _, mode := range []string{"off", "auto", "on"} {
		t.Run(mode, func(t *testing.T) {
			sub := &mockProvider{name: "vision"}
			task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).WithVision(mode, func(string) string { return "supported" }, nil)
			calls := 0
			ctx := WithTaskImageResolver(testTaskContext(), func(context.Context, []string, string) ([]provider.ImageContent, error) {
				calls++
				return nil, errors.New("image_send denied")
			})
			_, err := task.Execute(ctx, []byte(`{"prompt":"inspect","model":"vision","images":["frame.png"]}`))
			if err == nil || len(sub.requests) != 0 || (mode == "off" && calls != 0) || (mode != "off" && calls != 1) {
				t.Fatalf("err=%v requests=%d resolver=%d", err, len(sub.requests), calls)
			}
		})
	}
}

func TestTaskImageLegacyAttachmentsRequirePermissions(t *testing.T) {
	for _, denied := range []string{"read_file", "image_send"} {
		t.Run(denied, func(t *testing.T) {
			task := &TaskTool{visionMode: "on", baseModel: "provider/vision", gate: permission.NewGate(permission.New("allow", nil, nil, []string{denied}), nil), imageLoader: func(_ context.Context, im provider.ImageContent) (provider.ImageContent, error) {
				im.Data = "image"
				return im, nil
			}}
			ctx := WithTurnImages(context.Background(), []provider.ImageContent{{Path: "snapshot.png", Name: "frame.png"}})
			for _, ref := range []string{"frame.png", "snapshot.png"} {
				_, err := task.selectImages(ctx, []string{ref}, "provider/vision")
				if err == nil || !strings.Contains(err.Error(), "denied") {
					t.Fatalf("ref=%s err=%v", ref, err)
				}
			}
		})
	}
	task := &TaskTool{gate: permission.NewGate(permission.New("ask", nil, nil, nil), nil)}
	if err := task.checkImagePermission(context.Background(), "image_send", "provider/vision"); err == nil {
		t.Fatal("headless Ask silently allowed an image export")
	}
}

func TestFrozenTaskImagesRejectHistoricalUnselectedImages(t *testing.T) {
	images := []provider.ImageContent{{Path: "selected.png", Data: "immutable"}}
	loader := frozenTaskImageLoader(images)
	images[0].Data = "changed"
	got, err := loader(context.Background(), provider.ImageContent{Path: "selected.png"})
	if err != nil || got.Data != "immutable" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := loader(context.Background(), provider.ImageContent{Path: "other-session.png", Data: "stale"}); err == nil {
		t.Fatal("historical image accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader(ctx, provider.ImageContent{Path: "selected.png"}); err == nil {
		t.Fatal("cancelled image accepted")
	}
}

func TestTaskImageCountLimitBeforeResolver(t *testing.T) {
	task := &TaskTool{visionMode: "on"}
	ctx := WithTaskImageResolver(context.Background(), func(context.Context, []string, string) ([]provider.ImageContent, error) {
		t.Fatal("over-budget resolver invoked")
		return nil, nil
	})
	if _, err := task.selectImages(ctx, make([]string, 9), "vision"); err == nil {
		t.Fatal("count limit bypassed")
	}
	var schema map[string]any
	if err := json.Unmarshal(task.Schema(), &schema); err != nil {
		t.Fatal(err)
	}
}
