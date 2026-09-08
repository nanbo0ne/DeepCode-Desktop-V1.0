package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/localai"
)

func TestLocalAIDisabledRejectsStartAndResumeAtDesktopBoundary(t *testing.T) {
	a := NewApp()
	if _, err := a.StartLocalRuntimeInstall("cpu-x64"); !errors.Is(err, localai.ErrTemporarilyDisabled) {
		t.Fatalf("runtime install error = %v, want ErrTemporarilyDisabled", err)
	}
	if _, err := a.StartLocalModelDownload("qwen3.5-4b-q4-k-m"); !errors.Is(err, localai.ErrTemporarilyDisabled) {
		t.Fatalf("model download error = %v, want ErrTemporarilyDisabled", err)
	}
	if err := a.ResumeLocalDownload("missing"); !errors.Is(err, localai.ErrTemporarilyDisabled) {
		t.Fatalf("resume error = %v, want ErrTemporarilyDisabled", err)
	}
}

func TestLocalAIDisabledDoesNotBlockCustomLocalhostProvider(t *testing.T) {
	a := NewApp()
	cfg := &config.Config{
		Providers: []config.ProviderEntry{{Name: "custom-local", Kind: "openai", BaseURL: "http://127.0.0.1:11434/v1", Model: "custom-model"}},
	}
	providers, err := a.prepareLocalRuntimeProviders(context.Background(), cfg, "custom-local/custom-model")
	if err != nil {
		t.Fatalf("custom localhost provider was gated: %v", err)
	}
	if providers != nil {
		t.Fatalf("custom localhost provider unexpectedly created runtime providers: %+v", providers)
	}
	if a.localAI != nil {
		t.Fatal("custom localhost provider should not initialize the managed local AI manager")
	}
}

func TestLocalAIDisabledRejectsManagedProviderAutostart(t *testing.T) {
	a := NewApp()
	cfg := &config.Config{}
	if _, err := a.prepareLocalRuntimeProviders(context.Background(), cfg, localai.ProviderID+"/qwen3.5-4b-q4-k-m"); !errors.Is(err, localai.ErrTemporarilyDisabled) {
		t.Fatalf("managed provider autostart error = %v, want ErrTemporarilyDisabled", err)
	}
	if a.localAI != nil {
		t.Fatal("managed provider autostart should not initialize the manager")
	}
}

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
