package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/localai"
)

const computerUseConsentVersion = 1

func (a *App) computerUseAvailability() error {
	if a.computerUse == nil {
		return computeruse.ErrNotSupported
	}
	return a.computerUse.Availability()
}

type ComputerUseState struct {
	Capabilities   computeruse.Capabilities `json:"capabilities"`
	Session        computeruse.Session      `json:"session"`
	Approved       bool                     `json:"approved"`
	ConsentVersion int                      `json:"consentVersion"`
	ModelRef       string                   `json:"modelRef,omitempty"`
}

func (a *App) GetComputerUseState() ComputerUseState {
	state := ComputerUseState{}
	if a.computerUse != nil {
		state.Capabilities = a.computerUse.Capabilities()
		state.Session = a.computerUse.Current()
	}
	var cfg *config.Config
	var err error
	if state.Session.TabID != "" {
		cfg, err = a.computerTaskConfig(state.Session.TabID)
	} else {
		cfg, err = config.LoadForRoot(a.activeWorkspaceRoot())
	}
	if err == nil {
		state.Approved = cfg.Desktop.ComputerUseFullAccess && cfg.Desktop.ComputerUseConsent == computerUseConsentVersion
		state.ConsentVersion = cfg.Desktop.ComputerUseConsent
		state.ModelRef = strings.TrimSpace(cfg.Desktop.ComputerControlModel)
	}
	return state
}

func (a *App) SetComputerUseFullAccess(enabled bool) error {
	if enabled {
		if err := a.computerUseAvailability(); err != nil {
			return err
		}
	}
	if !enabled && a.computerUse != nil {
		_ = a.computerUse.Stop("authorization revoked")
	}
	return a.applyConfigOnly(func(c *config.Config) error {
		c.Desktop.ComputerUseFullAccess = enabled
		if enabled {
			c.Desktop.ComputerUseConsent = computerUseConsentVersion
		} else {
			c.Desktop.ComputerUseConsent = 0
		}
		return nil
	})
}

func (a *App) StartComputerUseSession(request computeruse.StartRequest) (computeruse.Session, error) {
	done, err := a.beginAppWork()
	if err != nil {
		return computeruse.Session{}, err
	}
	defer done()
	if err := a.computerUseAvailability(); err != nil {
		return computeruse.Session{}, err
	}
	if request.ResumeSessionID != "" {
		return computeruse.Session{}, fmt.Errorf("resume through computer_task so the isolated controller is restarted")
	}
	request, err = a.computerTaskRequest(request)
	if err != nil {
		return computeruse.Session{}, err
	}
	cfg, err := a.computerTaskConfig(request.TabID)
	if err != nil {
		return computeruse.Session{}, err
	}
	if !cfg.Desktop.ComputerUseFullAccess || cfg.Desktop.ComputerUseConsent != computerUseConsentVersion {
		return computeruse.Session{}, fmt.Errorf("使用电脑控制前需要在设置中完成一次完全访问授权")
	}
	modelRef, err := a.resolveComputerControlModel(cfg, request.ModelRef)
	if err != nil {
		return computeruse.Session{}, err
	}
	request.ModelRef = modelRef
	entry, err := a.computerProviderEntry(a.bootContext(), cfg, modelRef)
	if err != nil {
		return computeruse.Session{}, err
	}
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		return computeruse.Session{}, err
	}
	if err := qualifyComputerProvider(a.bootContext(), prov, entry); err != nil {
		return computeruse.Session{}, err
	}
	return a.computerUse.Start(a.bootContext(), request)
}

func (a *App) ObserveComputerUse() (computeruse.Observation, error) {
	if a.computerUse == nil {
		return computeruse.Observation{}, computeruse.ErrNotSupported
	}
	return a.computerUse.Observe(a.bootContext())
}

func (a *App) ExecuteComputerAction(action computeruse.Action) (computeruse.ActionResult, error) {
	if a.computerUse == nil {
		return computeruse.ActionResult{}, computeruse.ErrNotSupported
	}
	return a.computerUse.Execute(a.bootContext(), action)
}

func (a *App) PauseComputerUse() (computeruse.Session, error) {
	if a.computerUse == nil {
		return computeruse.Session{}, computeruse.ErrNotSupported
	}
	return a.computerUse.Pause()
}

func (a *App) ResumeComputerUse() (computeruse.Session, error) {
	done, err := a.beginAppWork()
	if err != nil {
		return computeruse.Session{}, err
	}
	defer done()
	if err := a.computerUseAvailability(); err != nil {
		return computeruse.Session{}, err
	}
	return a.computerUse.Resume(a.bootContext())
}

func (a *App) StopComputerUse() error {
	if a.computerUse == nil {
		return nil
	}
	return a.computerUse.Stop("stopped by user")
}

func (a *App) resolveComputerControlModel(cfg *config.Config, requested string) (string, error) {
	ref := strings.TrimSpace(requested)
	if ref == "" {
		ref = strings.TrimSpace(cfg.Desktop.ComputerControlModel)
	}
	if ref == "" {
		ref = cfg.ResolveVisionModelRef()
	}
	entry, ok := cfg.ResolveModel(ref)
	if ref == "" || !ok {
		return "", fmt.Errorf("unknown or unset computer control model %q", ref)
	}
	if entry.Name == localai.ProviderID {
		spec, ok := localai.ModelByID(entry.Model)
		if !ok || !spec.Vision || !spec.ToolUse {
			return "", fmt.Errorf("computer control model %q requires image and structured tool support", ref)
		}
	}
	// Cloud ability is measured separately before Start; metadata is not proof.
	return entry.Name + "/" + entry.Model, nil
}

func (a *App) computerTaskConfig(tabID string) (*config.Config, error) {
	a.mu.RLock()
	tab := a.tabs[strings.TrimSpace(tabID)]
	if tab == nil || strings.TrimSpace(tab.WorkspaceRoot) == "" {
		a.mu.RUnlock()
		return nil, fmt.Errorf("computer task requires a valid owning tab and workspace")
	}
	root := tab.WorkspaceRoot
	a.mu.RUnlock()
	return config.LoadForRoot(root)
}

func (a *App) onComputerUseEvent(event computeruse.Event) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "computer:session", event.Session)
		if event.Observation != nil {
			wruntime.EventsEmit(a.ctx, "computer:observation", event.Observation)
		}
		if event.Action != nil {
			wruntime.EventsEmit(a.ctx, "computer:action", event.Action)
		}
		if event.Kind == "cancelled" || event.Kind == "failed" || event.Kind == "succeeded" {
			wruntime.EventsEmit(a.ctx, "computer:stopped", event.Session)
		}
	}
	a.appendComputerTelemetry(event)
}

func (a *App) appendComputerTelemetry(event computeruse.Event) {
	type record struct {
		At         time.Time                `json:"at"`
		Kind       string                   `json:"kind"`
		SessionID  string                   `json:"sessionId"`
		State      computeruse.SessionState `json:"state"`
		ActionType string                   `json:"actionType,omitempty"`
		Window     string                   `json:"window,omitempty"`
		DurationMS int64                    `json:"durationMs,omitempty"`
		Success    bool                     `json:"success,omitempty"`
		Error      string                   `json:"error,omitempty"`
	}
	r := record{At: time.Now().UTC(), Kind: event.Kind, SessionID: event.Session.ID, State: event.Session.State}
	if event.Action != nil {
		r.ActionType = event.Action.Action.Type
		r.Window = event.Session.CurrentApp
		r.DurationMS = event.Action.DurationMS
		r.Success = event.Action.Success
		r.Error = event.Action.Error
	}
	body, err := json.Marshal(r)
	if err != nil {
		return
	}
	path := filepath.Join(filepath.Dir(config.UserConfigPath()), "telemetry", "computer-use.jsonl")
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(body, '\n'))
}
