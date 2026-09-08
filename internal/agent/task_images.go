package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// TaskImageResolver is a host-owned, turn-scoped capability. Model arguments
// cannot mint it. It authorizes both reading and sending to the selected model.
type TaskImageResolver func(context.Context, []string, string) ([]provider.ImageContent, error)

type taskImageResolverKey struct{}

func WithTaskImageResolver(ctx context.Context, resolve TaskImageResolver) context.Context {
	return context.WithValue(ctx, taskImageResolverKey{}, resolve)
}

func (t *TaskTool) checkImagePermission(ctx context.Context, name, subject string) error {
	if t.gate == nil {
		return fmt.Errorf("image permission gate is unavailable")
	}
	args, _ := json.Marshal(map[string]string{"path": subject})
	if gate, ok := t.gate.(*permission.Gate); ok && gate.Approver == nil && !gate.Bypass && gate.Policy.Decide(name, false, args) == permission.Ask {
		return fmt.Errorf("image approval requires an interactive host")
	}
	allow, reason, err := t.gate.Check(ctx, name, args, false)
	if err != nil {
		return err
	}
	if !allow {
		return fmt.Errorf("%s denied: %s", name, reason)
	}
	return ctx.Err()
}

// Pin the authorized bytes for every request in this subagent, including after
// cache eviction. Historical images must be explicitly selected again.
func frozenTaskImageLoader(images []provider.ImageContent) func(context.Context, provider.ImageContent) (provider.ImageContent, error) {
	frozen := make(map[string]provider.ImageContent, len(images))
	for _, image := range images {
		frozen[image.Path] = image
	}
	return func(ctx context.Context, image provider.ImageContent) (provider.ImageContent, error) {
		if err := ctx.Err(); err != nil {
			return provider.ImageContent{}, err
		}
		if pinned, ok := frozen[image.Path]; ok && pinned.Data != "" {
			return pinned, nil
		}
		return provider.ImageContent{}, fmt.Errorf("image was not authorized for this task")
	}
}
