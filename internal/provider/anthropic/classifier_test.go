package anthropic

import (
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"testing"
)

func TestClassifierOmitsAdaptiveThinkingWithoutChangingChat(t *testing.T) {
	c := &client{model: "test", thinking: "adaptive", effort: "max"}
	req := c.buildRequest(provider.Request{DisableThinking: true, MaxTokens: 256})
	if req.Thinking != nil || req.OutputConfig != nil || req.MaxTokens != 256 {
		t.Fatalf("classifier inherits thinking: %+v", req)
	}
	normal := c.buildRequest(provider.Request{})
	if normal.Thinking == nil || normal.Thinking.Type != "adaptive" || normal.OutputConfig == nil || normal.OutputConfig.Effort != "max" {
		t.Fatal("classifier changed ordinary chat")
	}
}
