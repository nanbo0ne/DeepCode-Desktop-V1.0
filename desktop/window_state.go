package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

// DesktopWindowState captures the window geometry to restore across launches.
type DesktopWindowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Maximised bool `json:"maximised"`
}

func windowStatePath() string {
	return filepath.Join(config.MemoryUserDir(), "desktop-window.json")
}

// loadWindowState reads the saved window geometry. The second return value is
// false when no saved state exists (first launch, missing file, corrupt JSON).
// Callers must not restore position when ok is false — zero values are not a
// valid window origin.
func loadWindowState() (DesktopWindowState, bool) {
	path := windowStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return DesktopWindowState{}, false
	}
	var s DesktopWindowState
	if err := json.Unmarshal(data, &s); err != nil {
		return DesktopWindowState{}, false
	}
	if s.Width < 400 {
		s.Width = 0
	}
	if s.Height < 300 {
		s.Height = 0
	}
	// Migration guard: the previous version saved (0,0) for first-launch zero
	// values. Treat an all-zero valid file the same as missing — let domReady
	// center the window instead of parking it at the screen origin.
	if s.Width == 0 && s.Height == 0 && s.X == 0 && s.Y == 0 {
		return DesktopWindowState{}, false
	}
	return s, true
}

// SaveWindowState is the bound method the frontend calls to persist the current
// window geometry periodically during use. Once beforeClose has completed the
// final native capture, late IPC calls are ignored so they cannot overwrite it.
func (a *App) SaveWindowState(state DesktopWindowState) error {
	a.windowStateMu.Lock()
	defer a.windowStateMu.Unlock()
	if a.windowStateClosing.Load() {
		return nil
	}
	return a.saveWindowStateLocked(state)
}

func (a *App) saveWindowStateLocked(state DesktopWindowState) error {
	path := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// saveWindowStateSync captures the native geometry while ctx still refers to a
// live window. The lock spans the native reads and disk write, so an async
// frontend save cannot land after this final capture. closeAfter prevents any
// later frontend IPC from overwriting the final state.
func (a *App) saveWindowStateSync(ctx context.Context, closeAfter bool) (DesktopWindowState, bool) {
	a.windowStateMu.Lock()
	defer a.windowStateMu.Unlock()
	if !windowStateContextIsLive(ctx) || a.windowStateClosing.Load() {
		return DesktopWindowState{}, false
	}
	w, h := runtime.WindowGetSize(ctx)
	x, y := runtime.WindowGetPosition(ctx)
	state := DesktopWindowState{Width: w, Height: h, X: x, Y: y, Maximised: runtime.WindowIsMaximised(ctx)}
	_ = a.saveWindowStateLocked(state)
	if closeAfter {
		a.windowStateClosing.Store(true)
	}
	return state, true
}

func (a *App) closeWindowStateSaves() {
	a.windowStateMu.Lock()
	a.windowStateClosing.Store(true)
	a.windowStateMu.Unlock()
}

// Wails runtime functions terminate the process when the lifecycle context is
// missing its frontend value. Check that contract before touching any native
// window API; this also keeps non-Wails unit callers fail-closed.
func windowStateContextIsLive(ctx context.Context) bool {
	return ctx != nil && ctx.Value("frontend") != nil
}
