package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type taskImageGateFunc func(context.Context, string, json.RawMessage, bool) (bool, string, error)

func (f taskImageGateFunc) Check(ctx context.Context, name string, args json.RawMessage, ro bool) (bool, string, error) {
	return f(ctx, name, args, ro)
}

func taskImagePNG(t *testing.T, red uint8) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	im.SetNRGBA(0, 0, color.NRGBA{R: red, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func putTaskImage(t *testing.T, root, name string, raw []byte) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func allowTaskImages() agent.Gate {
	return permission.NewGate(permission.New("allow", nil, nil, nil), nil)
}

func TestTaskImagesGeneratedSnapshotAndSendTOCTOU(t *testing.T) {
	root := t.TempDir()
	raw := taskImagePNG(t, 20)
	path := putTaskImage(t, root, "frames/one.png", raw)
	var operations []string
	gate := taskImageGateFunc(func(_ context.Context, name string, args json.RawMessage, ro bool) (bool, string, error) {
		operations = append(operations, name)
		if ro {
			t.Fatal("host image access used read-only automatic permission")
		}
		if name == "image_send" {
			var manifest struct {
				Path   string `json:"path"`
				Images []struct {
					Bytes  int64  `json:"bytes"`
					SHA256 string `json:"sha256"`
				} `json:"images"`
			}
			if err := json.Unmarshal(args, &manifest); err != nil || manifest.Path != "selected/vision" || len(manifest.Images) != 1 || manifest.Images[0].Bytes != int64(len(raw)) || len(manifest.Images[0].SHA256) != 64 {
				t.Fatalf("incorrect destination/content approval: %s (%v)", args, err)
			}
			// Simulate replacement while the user reviews the send approval.
			if err := os.WriteFile(path, taskImagePNG(t, 240), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return true, "", nil
	})
	images, total, err := resolveTaskImages(context.Background(), root, []string{"frames/one.png"}, "selected/vision", nil, gate, 8, maxVisionImageBytesPerTurn)
	if err != nil || len(images) != 1 || total != int64(len(raw)) {
		t.Fatalf("images=%+v total=%d err=%v", images, total, err)
	}
	if strings.Join(operations, ",") != "read_file,image_send" {
		t.Fatalf("permission sequence=%v", operations)
	}
	if images[0].Data != base64.StdEncoding.EncodeToString(raw) || !strings.HasPrefix(images[0].Path, ".orca/attachments/") {
		t.Fatal("snapshot did not preserve authorized bytes")
	}
	saved, err := os.ReadFile(filepath.Join(root, images[0].Path))
	if err != nil || !bytes.Equal(saved, raw) {
		t.Fatalf("saved bytes differ: %v", err)
	}
}

func TestTaskImagesDenyPathsFormatsAndBudgets(t *testing.T) {
	root := t.TempDir()
	raw := taskImagePNG(t, 20)
	putTaskImage(t, root, "frame.png", raw)
	putTaskImage(t, root, "not-image.png", []byte("not an image"))
	putTaskImage(t, root, "truncated.png", raw[:len(raw)/2])
	putTaskImage(t, root, "oversize.png", make([]byte, maxImageAttachmentBytes+1))
	putTaskImage(t, root, ".orca/attachments/other-session.png", raw)
	outside := putTaskImage(t, t.TempDir(), "private.png", raw)
	for _, tc := range []struct {
		name   string
		paths  []string
		gate   agent.Gate
		count  int
		budget int64
	}{
		{"read denied", []string{"frame.png"}, permission.NewGate(permission.New("allow", nil, nil, []string{"read_file"}), nil), 8, maxVisionImageBytesPerTurn},
		{"send denied", []string{"frame.png"}, permission.NewGate(permission.New("allow", nil, nil, []string{"image_send(selected/vision)"}), nil), 8, maxVisionImageBytesPerTurn},
		{"no gate", []string{"frame.png"}, nil, 8, maxVisionImageBytesPerTurn},
		{"outside", []string{outside}, allowTaskImages(), 8, maxVisionImageBytesPerTurn},
		{"traversal", []string{"../private.png"}, allowTaskImages(), 8, maxVisionImageBytesPerTurn},
		{"cross session", []string{".orca/attachments/other-session.png"}, allowTaskImages(), 8, maxVisionImageBytesPerTurn},
		{"invalid format", []string{"not-image.png"}, allowTaskImages(), 8, maxVisionImageBytesPerTurn},
		{"truncated format", []string{"truncated.png"}, allowTaskImages(), 8, maxVisionImageBytesPerTurn},
		{"oversize", []string{"oversize.png"}, allowTaskImages(), 8, maxVisionImageBytesPerTurn},
		{"count", []string{"frame.png"}, allowTaskImages(), 0, maxVisionImageBytesPerTurn},
		{"total bytes", []string{"frame.png"}, allowTaskImages(), 8, int64(len(raw) - 1)},
		{"alternate stream", []string{"frame.png:private"}, allowTaskImages(), 8, maxVisionImageBytesPerTurn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			images, _, err := resolveTaskImages(context.Background(), root, tc.paths, "selected/vision", nil, tc.gate, tc.count, tc.budget)
			if err == nil || len(images) != 0 {
				t.Fatalf("denial failed: images=%+v err=%v", images, err)
			}
		})
	}
}

func TestTaskImagesRealTotalBudgetAndPixelLimit(t *testing.T) {
	root := t.TempDir()
	raw := taskImagePNG(t, 20)
	large := make([]byte, 8*1024*1024)
	copy(large, raw)
	for _, name := range []string{"one.png", "two.png", "three.png"} {
		putTaskImage(t, root, name, large)
	}
	sends := 0
	gate := taskImageGateFunc(func(_ context.Context, name string, _ json.RawMessage, _ bool) (bool, string, error) {
		if name == "image_send" {
			sends++
		}
		return true, "", nil
	})
	if _, _, err := resolveTaskImages(context.Background(), root, []string{"one.png", "two.png", "three.png"}, "selected/vision", nil, gate, 8, maxVisionImageBytesPerTurn); err == nil || sends != 0 {
		t.Fatalf("24 MB export was not stopped before send approval: %v sends=%d", err, sends)
	}
	// A valid IHDR with huge dimensions must be rejected before pixel allocation.
	oversized := append([]byte(nil), raw...)
	binary.BigEndian.PutUint32(oversized[16:20], 32769)
	binary.BigEndian.PutUint32(oversized[20:24], 1024)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	if _, err := validateTaskImage(oversized); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("pixel limit not enforced: %v", err)
	}
}

func TestTaskImagesLinksRejected(t *testing.T) {
	for _, kind := range []string{"file symlink", "directory symlink", "hard link"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			target := putTaskImage(t, t.TempDir(), "private.png", taskImagePNG(t, 20))
			name := "link.png"
			var err error
			switch kind {
			case "file symlink":
				err = os.Symlink(target, filepath.Join(root, name))
			case "directory symlink":
				err = os.Symlink(filepath.Dir(target), filepath.Join(root, "linked"))
				name = "linked/private.png"
			case "hard link":
				err = os.Link(target, filepath.Join(root, name))
			}
			if err != nil {
				t.Skipf("link creation unavailable: %v", err)
			}
			if _, _, err := resolveTaskImages(context.Background(), root, []string{name}, "selected/vision", nil, allowTaskImages(), 8, maxVisionImageBytesPerTurn); err == nil {
				t.Fatal("link accepted")
			}
		})
	}
}

func TestTaskImagesExistingAttachmentAliasesUseFrozenBytes(t *testing.T) {
	root := t.TempDir()
	raw := taskImagePNG(t, 20)
	c := &Controller{cpRoot: root}
	path := putTaskImage(t, root, "original.png", raw)
	attached, err := c.prepareVisionImage(path)
	if err != nil {
		t.Fatal(err)
	}
	putTaskImage(t, root, attached.Path, taskImagePNG(t, 240))
	for _, name := range []string{attached.Name, attached.Path} {
		images, _, err := resolveTaskImages(context.Background(), root, []string{name}, "selected/vision", []provider.ImageContent{attached}, allowTaskImages(), 8, maxVisionImageBytesPerTurn)
		if err != nil || len(images) != 1 || images[0].Data != base64.StdEncoding.EncodeToString(raw) {
			t.Fatalf("alias %q lost frozen attachment: %+v %v", name, images, err)
		}
	}
}

type taskImageCaptureProvider struct{ requests []provider.Request }

func (*taskImageCaptureProvider) Name() string { return "vision" }
func (p *taskImageCaptureProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "inspected"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func controllerImageTask(t *testing.T, root string, p provider.Provider) *agent.TaskTool {
	t.Helper()
	return agent.NewTaskTool(p, nil, tool.NewRegistry(), 5, 0, 0, 0, 0, 0, "", "sys", nil, "", "", nil).
		WithTranscripts(agent.NewSubagentStore(t.TempDir()), root, "selected/vision", "").
		WithVision("auto", func(string) string { return "supported" }, nil)
}

func TestControllerTaskImagesApprovalModesAndScope(t *testing.T) {
	for _, tc := range []struct {
		name, mode, deny string
		reject, wantErr  bool
		prompts          int
	}{
		{"manual approve", ToolApprovalAsk, "", false, false, 2},
		{"manual refuse", ToolApprovalAsk, "", true, true, 1},
		{"auto", ToolApprovalAuto, "", false, false, 0},
		{"auto explicit ask", ToolApprovalAuto, "", false, false, 1},
		{"auto deny read", ToolApprovalAuto, "read_file", false, true, 0},
		{"auto deny relative read", ToolApprovalAuto, "read_file(frame.png)", false, true, 0},
		{"auto deny send", ToolApprovalAuto, "image_send", false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			putTaskImage(t, root, "frame.png", taskImagePNG(t, 10))
			deny := []string{}
			if tc.deny != "" {
				deny = append(deny, tc.deny)
			}
			ask := []string{}
			if tc.name == "auto explicit ask" {
				ask = append(ask, "image_send")
			}
			c := New(Options{WorkspaceRoot: root, Policy: permission.New("ask", nil, ask, deny), VisionMode: "auto"})
			c.SetToolApprovalMode(tc.mode)
			prompts := 0
			c.sink = event.FuncSink(func(e event.Event) {
				if e.Kind == event.ApprovalRequest {
					prompts++
					c.Approve(e.Approval.ID, !tc.reject, false, false)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ctx, end := agent.WithParentTurn(agent.WithParentSession(ctx, "owner"))
			defer end()
			ctx = c.withTaskImages(ctx, true)
			p := &taskImageCaptureProvider{}
			task := controllerImageTask(t, root, p)
			args := []byte(`{"prompt":"inspect","images":["frame.png"]}`)
			_, err := task.Execute(ctx, args)
			if (err != nil) != tc.wantErr || prompts != tc.prompts || (tc.wantErr && len(p.requests) != 0) {
				t.Fatalf("err=%v prompts=%d requests=%d", err, prompts, len(p.requests))
			}
			if _, err := task.Execute(agent.WithParentSession(ctx, "other-owner"), args); err == nil {
				t.Fatal("resolver reused across sessions")
			}
			end()
			if _, err := task.Execute(ctx, args); err == nil {
				t.Fatal("resolver reused after turn expired")
			}
		})
	}
}

type generatedFrameTool struct {
	fakeControlTool
	root string
	raw  []byte
}

func (g generatedFrameTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "frame.png", os.WriteFile(filepath.Join(g.root, "frame.png"), g.raw, 0o600)
}
func (generatedFrameTool) ReadOnly() bool { return false }

func TestControllerRootProducesFrameThenVisionTaskWithoutAttachment(t *testing.T) {
	for _, headless := range []bool{false, true} {
		t.Run(fmt.Sprint("headless=", headless), func(t *testing.T) {
			root := t.TempDir()
			raw := taskImagePNG(t, 42)
			vision := &taskImageCaptureProvider{}
			reg := tool.NewRegistry()
			reg.Add(generatedFrameTool{fakeControlTool{name: "produce_frame"}, root, raw})
			reg.Add(controllerImageTask(t, root, vision))
			parent := &scriptedTurns{turns: [][]provider.Chunk{
				{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "produce", Name: "produce_frame", Arguments: `{}`}}, {Type: provider.ChunkDone}},
				{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "inspect", Name: "task", Arguments: `{"prompt":"inspect the frame","images":["frame.png"]}`}}, {Type: provider.ChunkDone}},
				textTurn("done"),
			}}
			ag := agent.New(parent, reg, agent.NewSession("sys"), agent.Options{MaxSteps: 5}, event.Discard)
			c := New(Options{Runner: ag, Executor: ag, WorkspaceRoot: root, VisionMode: "auto", Policy: permission.New("allow", []string{"read_file", "image_send"}, nil, nil)})
			var err error
			if headless {
				err = c.Run(context.Background(), "produce and inspect a frame")
			} else {
				err = c.RunTurn(context.Background(), "produce and inspect a frame")
			}
			if err != nil || len(vision.requests) != 1 {
				t.Fatalf("err=%v vision requests=%d parent calls=%d", err, len(vision.requests), parent.call)
			}
			last := vision.requests[0].Messages[len(vision.requests[0].Messages)-1]
			if len(last.Images) != 1 || last.Images[0].Data != base64.StdEncoding.EncodeToString(raw) {
				t.Fatalf("generated frame absent from subagent: %+v", last.Images)
			}
		})
	}
}

func TestControllerTaskImagesCumulativeBudgetAndHeadlessAsk(t *testing.T) {
	root := t.TempDir()
	putTaskImage(t, root, "frame.png", taskImagePNG(t, 10))
	for _, mode := range []string{"allow", "ask"} {
		t.Run(mode, func(t *testing.T) {
			c := New(Options{WorkspaceRoot: root, VisionMode: "auto", Policy: permission.New(mode, nil, nil, nil)})
			ctx, end := agent.WithParentTurn(context.Background())
			defer end()
			ctx = c.withTaskImages(ctx, false)
			p := &taskImageCaptureProvider{}
			task := controllerImageTask(t, root, p)
			for i := 0; i < 9; i++ {
				// Automatic goal continuations reuse this parent turn context.
				ctx = c.withTaskImages(ctx, false)
				_, err := task.Execute(ctx, []byte(`{"prompt":"inspect","images":["frame.png"]}`))
				wantErr := mode == "ask" || i == 8
				if (err != nil) != wantErr {
					t.Fatalf("call=%d err=%v wantErr=%v", i, err, wantErr)
				}
			}
		})
	}
}

func TestControllerTaskImagesVisionOffAndCancellation(t *testing.T) {
	root := t.TempDir()
	putTaskImage(t, root, "frame.png", taskImagePNG(t, 10))
	for _, off := range []bool{true, false} {
		c := New(Options{WorkspaceRoot: root, VisionMode: "auto", Policy: permission.New("allow", nil, nil, nil)})
		if off {
			c.visionMode = "off"
		}
		ctx, end := agent.WithParentTurn(context.Background())
		ctx = c.withTaskImages(ctx, false)
		if !off {
			end()
		}
		p := &taskImageCaptureProvider{}
		_, err := controllerImageTask(t, root, p).Execute(ctx, []byte(`{"prompt":"inspect","images":["frame.png"]}`))
		end()
		if err == nil || len(p.requests) != 0 {
			t.Fatalf("off=%v err=%v requests=%d", off, err, len(p.requests))
		}
	}
}
