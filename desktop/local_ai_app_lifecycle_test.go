package main

import (
	"os"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/localai"
)

func TestLocalRuntimeProviderBindsTokenWithoutProcessEnvironment(t *testing.T) {
	const sentinel = "pre-existing-process-value"
	t.Setenv(localai.ProviderKeyEnv, sentinel)

	entry := localRuntimeProviderEntry("qwen3.5-4b-q4-k-m", localai.RuntimeStatus{
		BaseURL: "http://127.0.0.1:43210/v1",
		APIKey:  "runtime-session-token",
		Profile: localai.LoadProfile{ContextSize: 8192},
	})
	if entry.APIKeyEnv != "" {
		t.Fatalf("runtime provider APIKeyEnv = %q, want empty", entry.APIKeyEnv)
	}
	if got := entry.APIKey(); got != "runtime-session-token" {
		t.Fatalf("runtime provider API key = %q, want in-memory token", got)
	}
	if got := os.Getenv(localai.ProviderKeyEnv); got != sentinel {
		t.Fatalf("runtime provider changed %s to %q, want existing value preserved", localai.ProviderKeyEnv, got)
	}
}
