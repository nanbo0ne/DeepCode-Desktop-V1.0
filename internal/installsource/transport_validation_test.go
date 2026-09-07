package installsource

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestUnsupportedTransportRejectedBeforeApproval(t *testing.T) {
	for _, apply := range []bool{false, true} {
		approved := false
		tool := NewTool(Options{ProjectRoot: t.TempDir(), HomeDir: t.TempDir(), Approval: func([]action) error {
			approved = true
			return nil
		}})
		raw, _ := json.Marshal(map[string]any{"kind": "mcp", "source": "https://example.com/sse", "transport": "sse", "apply": apply})
		_, err := tool.Execute(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), "Streamable HTTP") {
			t.Fatalf("apply=%v: expected unsupported transport error, got %v", apply, err)
		}
		if approved {
			t.Fatal("unsupported transport reached approval")
		}
	}
}
