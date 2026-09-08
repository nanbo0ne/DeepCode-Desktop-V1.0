package control

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// No interactive consumer exists in headless mode. Unlike ordinary autonomous
// tools, a new image export must not silently turn an Ask into an Allow.
type taskImageHeadlessApprover struct{}

type taskImageScopeKey struct{}
type taskImageScope struct {
	controller *Controller
	session    string
	turn       string
}

func (taskImageHeadlessApprover) Approve(context.Context, string, string, json.RawMessage) (bool, bool, error) {
	return false, false, fmt.Errorf("image approval requires an interactive user or an explicit allow policy")
}

func (c *Controller) withTaskImages(ctx context.Context, interactive bool) context.Context {
	owner := agent.ParentSession(ctx)
	turn, lifetime := agent.ParentTurn(ctx)
	if scope, ok := ctx.Value(taskImageScopeKey{}).(taskImageScope); ok && scope.controller == c && scope.session == owner && scope.turn == turn {
		return ctx
	}
	available := agent.TurnImages(ctx)
	workspace := c.cpRoot
	var mu sync.Mutex
	var count int
	var total int64
	ctx = agent.WithTaskImageResolver(ctx, func(call context.Context, names []string, model string) ([]provider.ImageContent, error) {
		mu.Lock()
		defer mu.Unlock()
		callTurn, _ := agent.ParentTurn(call)
		if turn == "" || lifetime == nil || lifetime.Err() != nil || callTurn != turn || agent.ParentSession(call) != owner {
			return nil, fmt.Errorf("image resolver belongs to a different or expired session/turn")
		}
		if c.visionMode == "off" {
			return nil, fmt.Errorf("vision is disabled in Settings")
		}
		var gate agent.Gate
		if interactive {
			gate = c.newInteractiveGate()
		} else {
			gate = permission.NewGate(c.policy, taskImageHeadlessApprover{})
		}
		gate = taskImagePolicyGate{Gate: gate, policy: c.policy, workspace: workspace}
		images, bytes, err := resolveTaskImages(call, workspace, names, model, available, gate,
			maxVisionImagesPerTurn-count, maxVisionImageBytesPerTurn-total)
		if err != nil {
			return nil, err
		}
		count += len(images)
		total += bytes
		return images, nil
	})
	return context.WithValue(ctx, taskImageScopeKey{}, taskImageScope{controller: c, session: owner, turn: turn})
}

// File rules may use either workspace-relative or absolute subjects. Preserve
// explicit deny/ask rules in both forms without asking twice for one read.
type taskImagePolicyGate struct {
	agent.Gate
	policy    permission.Policy
	workspace string
}

func (g taskImagePolicyGate) Check(ctx context.Context, name string, args json.RawMessage, ro bool) (bool, string, error) {
	if name == "read_file" {
		var values map[string]any
		if err := json.Unmarshal(args, &values); err != nil {
			return false, "invalid image permission", err
		}
		path, _ := values["path"].(string)
		_, original := g.policy.DecideSubjectDetailed(name, ro, path)
		rel, err := taskImageRelativePath(g.workspace, path)
		if err != nil {
			return false, "invalid image path", err
		}
		for _, alias := range []string{rel, filepath.ToSlash(rel)} {
			_, source := g.policy.DecideSubjectDetailed(name, ro, alias)
			if source == permission.DecisionDenyRule || (source == permission.DecisionAskRule && original != permission.DecisionDenyRule) {
				values["path"] = alias
				args, _ = json.Marshal(values)
				original = source
			}
		}
	}
	return g.Gate.Check(ctx, name, args, ro)
}

func checkTaskImagePermission(ctx context.Context, gate agent.Gate, name string, args any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if gate == nil {
		return fmt.Errorf("image permission gate is unavailable")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	// A read-only default is not authorization to read/export host image bytes.
	allow, reason, err := gate.Check(ctx, name, raw, false)
	if err != nil {
		return err
	}
	if !allow {
		return fmt.Errorf("%s denied: %s", name, reason)
	}
	return ctx.Err()
}

func resolveTaskImages(ctx context.Context, workspace string, names []string, model string, available []provider.ImageContent, gate agent.Gate, countBudget int, byteBudget int64) ([]provider.ImageContent, int64, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(model) == "" {
		return nil, 0, fmt.Errorf("image resolution requires a workspace and selected model identity")
	}
	if len(names) == 0 || len(names) > countBudget || len(names) > maxVisionImagesPerTurn {
		return nil, 0, fmt.Errorf("image count exceeds the remaining turn budget")
	}
	root, err := openTaskImageRoot(workspace)
	if err != nil {
		return nil, 0, err
	}
	defer root.Close()
	var images []provider.ImageContent
	var total int64
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		var attached *provider.ImageContent
		for i := range available {
			if name != "" && (name == available[i].Name || name == available[i].Path) {
				if attached != nil && attached.Path != available[i].Path {
					return nil, 0, fmt.Errorf("ambiguous image name %q; use its snapshot path", name)
				}
				attached = &available[i]
			}
		}
		path := name
		if attached != nil {
			path = attached.Path
		}
		rel, err := taskImageRelativePath(workspace, path)
		if err != nil {
			return nil, 0, err
		}
		// Private snapshots and transcript storage are not a workspace-image API.
		// Only references explicitly carried by this user turn can name them.
		first := strings.ToLower(strings.Split(filepath.ToSlash(rel), "/")[0])
		if attached == nil && (first == ".orca" || first == ".deepseek-orca" || first == ".git") {
			return nil, 0, fmt.Errorf("image %q is not a current-session attachment", name)
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if err := checkTaskImagePermission(ctx, gate, "read_file", map[string]any{"path": filepath.Join(workspace, rel), "workspace_path": filepath.ToSlash(rel), "purpose": "task image snapshot"}); err != nil {
			return nil, 0, err
		}
		var raw []byte
		if attached != nil && attached.Data != "" {
			if len(attached.Data) > base64.StdEncoding.EncodedLen(maxImageAttachmentBytes) {
				return nil, 0, fmt.Errorf("image exceeds 10 MB")
			}
			raw, err = base64.StdEncoding.DecodeString(attached.Data)
		} else {
			raw, err = readTaskImage(root, rel)
		}
		if err != nil {
			return nil, 0, err
		}
		mime, err := validateTaskImage(raw)
		if err != nil {
			return nil, 0, err
		}
		total += int64(len(raw))
		if total > byteBudget {
			return nil, 0, fmt.Errorf("images exceed the remaining 20 MB turn budget")
		}
		images = append(images, provider.ImageContent{Path: filepath.ToSlash(rel), Name: filepath.Base(name), MediaType: mime, Size: int64(len(raw)), Data: base64.StdEncoding.EncodeToString(raw)})
	}
	manifest := make([]map[string]any, 0, len(images))
	for _, image := range images {
		raw, _ := base64.StdEncoding.DecodeString(image.Data)
		manifest = append(manifest, map[string]any{"path": image.Path, "bytes": image.Size, "sha256": fmt.Sprintf("%x", sha256.Sum256(raw))})
	}
	if err := checkTaskImagePermission(ctx, gate, "image_send", map[string]any{"path": model, "destination_model": model, "images": manifest}); err != nil {
		return nil, 0, err
	}
	// Snapshot the exact approved bytes, never reopen their source after approval.
	for i := range images {
		raw, _ := base64.StdEncoding.DecodeString(images[i].Data)
		path, err := snapshotTaskImage(root, imageExt(images[i].MediaType), raw)
		if err != nil {
			return nil, 0, err
		}
		images[i].Path = path
	}
	return images, total, nil
}
