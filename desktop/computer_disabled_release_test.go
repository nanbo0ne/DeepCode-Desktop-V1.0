package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestDisabledComputerReleaseBlocksBridgeAndStaleTool(t *testing.T) {
	a := &App{computerUse: computeruse.NewService(computeruse.NewPlatformBackend(), nil)}
	if len(a.computerTools("owner")) != 0 {
		t.Fatal("disabled tools advertised to agent profiles")
	}
	_, startErr := a.StartComputerUseSession(computeruse.StartRequest{Goal: "test"})
	_, resumeErr := a.ResumeComputerUse()
	_, observeErr := a.ObserveComputerUse()
	_, actionErr := a.ExecuteComputerAction(computeruse.Action{Type: "click"})
	_, toolErr := (computerTaskTool{app: a, tabID: "owner"}).Execute(context.Background(), []byte(`{"goal":"test"}`))
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		t.Fatal("disabled control uploaded a provider request")
		return nil, nil
	}}
	_, providerErr := a.runComputerTaskWithProvider(computerParentTestContext(t), computeruse.StartRequest{TabID: "owner"}, p)
	for _, err := range []error{startErr, resumeErr, observeErr, actionErr, toolErr, providerErr, a.SetComputerUseFullAccess(true)} {
		if !errors.Is(err, computeruse.ErrTemporarilyDisabled) {
			t.Fatalf("disabled bridge/tool allowed work: %v", err)
		}
	}
	if err := a.StopComputerUse(); err != nil {
		t.Fatalf("stop must remain available: %v", err)
	}
}

func TestDisabledComputerReleasePreservesSavedPreferences(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Desktop.ComputerUseFullAccess = true
	cfg.Desktop.ComputerUseConsent = computerUseConsentVersion
	cfg.Desktop.ComputerControlModel = "local/qwen"
	cfg.Agent.SubagentModels = map[string]string{"vision": "deepseek/deepseek-v4-flash-vision-exp"}
	path := config.UserConfigPath()
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.computerUse = computeruse.NewService(computeruse.NewPlatformBackend(), nil)
	if err := a.SetComputerUseFullAccess(true); !errors.Is(err, computeruse.ErrTemporarilyDisabled) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("disabled authorization changed saved configuration", err)
	}
	if err := a.SetComputerUseFullAccess(false); err != nil {
		t.Fatal(err)
	}
	got := config.LoadForEdit(path)
	if got.Desktop.ComputerUseFullAccess || got.Desktop.ComputerControlModel != "local/qwen" || got.Agent.SubagentModels["vision"] != cfg.Agent.SubagentModels["vision"] {
		t.Fatal("revocation damaged saved model preferences")
	}
}
