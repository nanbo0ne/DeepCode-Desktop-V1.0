// Command Orca is the Wails shell around the O.R.C.A kernel: a native
// window hosting a webview frontend, with the Go-side control.Controller bound
// directly to the UI (no HTTP hop — bindings in, runtime events out). It lives in
// a nested module (github.com/nanbo0ne/O.R.C.A-for-Windows/desktop) so the CGO/WebKit desktop build never touches
// the CLI's CGO_ENABLED=0 single-static-binary guarantee, while still importing
// the same internal/* kernel.
package main

import (
	"embed"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	// Blank imports wire compile-time built-ins into their registries, exactly as
	// The CLI entry point does — boot.Build resolves providers/tools from these registries.
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/product"
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/anthropic"
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/openai"
	_ "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool/builtin"
)

// assets embeds the built frontend. `all:` so dotfiles (e.g. the dist .gitkeep
// that keeps this directive compilable before the first `pnpm build`) are
// included. A real run requires `pnpm build` (or `wails build`) to populate dist.
//
//go:embed all:frontend/dist
var assets embed.FS

// version is injected at build time via `wails build -ldflags "-X main.version=..."`,
// mirroring cmd/orca/main.go. The auto-updater reads it (App.Version) to compare
// against the published manifest; an un-injected dev build stays "dev" and never
// prompts to update.
var version = "dev"

// channel selects which updater pointer this build polls, injected via
// `-X main.channel=canary`. Default "stable" tracks the public release; "canary"
// tracks the opt-in pre-release line and never crosses over to stable.
var channel = "stable"

const disableWebview2GPUEnv = "ORCA_DESKTOP_DISABLE_WEBVIEW2_GPU"
const restartWaitArgPrefix = "--orca-restart-wait-ms="

func consumeRestartWaitArg(args []string) ([]string, time.Duration) {
	clean := make([]string, 0, len(args))
	var wait time.Duration
	for _, arg := range args {
		if !strings.HasPrefix(arg, restartWaitArgPrefix) {
			clean = append(clean, arg)
			continue
		}
		milliseconds, err := strconv.Atoi(strings.TrimPrefix(arg, restartWaitArgPrefix))
		if err == nil && milliseconds > 0 && milliseconds <= 10_000 {
			wait = time.Duration(milliseconds) * time.Millisecond
		}
	}
	return clean, wait
}

func desktopWindowTitle(goos string) string {
	if goos == "windows" {
		return product.WindowsName
	}
	return product.Name
}

func desktopFrameless(goos, style string) bool {
	return goos == "windows" && !strings.EqualFold(strings.TrimSpace(style), config.DesktopUIStyleClassic)
}

func desktopBackground(style string) *options.RGBA {
	if strings.EqualFold(strings.TrimSpace(style), config.DesktopUIStyleClassic) {
		return &options.RGBA{R: 251, G: 252, B: 255, A: 255}
	}
	return &options.RGBA{R: 255, G: 255, B: 255, A: 255}
}

func windowsWebview2GPUDisabled() bool {
	if raw, ok := os.LookupEnv(disableWebview2GPUEnv); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off", "":
			return false
		}
	}
	return channel == "canary"
}

func main() {
	var restartWait time.Duration
	os.Args, restartWait = consumeRestartWaitArg(os.Args)
	if restartWait > 0 {
		time.Sleep(restartWait)
	}
	addExecutableDirToPATH()

	app := NewApp()
	desktopStyle := config.LoadForEdit(config.UserConfigPath()).DesktopUIStyle()

	// Restore saved window size, or fall back to the default.
	width, height := 1240, 720
	if saved, ok := loadWindowState(); ok {
		if saved.Width > 0 {
			width = saved.Width
		}
		if saved.Height > 0 {
			height = saved.Height
		}
	}

	err := wails.Run(&options.App{
		Title:     desktopWindowTitle(runtime.GOOS),
		Width:     width,
		Height:    height,
		MinWidth:  760,
		MinHeight: 480,
		// Modern owns its Windows title bar. Classic deliberately keeps the native
		// V2.1.3 frame, so changing styles is applied on the next process start.
		Frameless: desktopFrameless(runtime.GOOS, desktopStyle),
		// Match the selected presentation so the initial webview background does not
		// flash to a conflicting color before CSS loads.
		BackgroundColour:   desktopBackground(desktopStyle),
		AssetServer:        &assetserver.Options{Assets: assets, Middleware: app.workspaceMediaMiddleware()},
		OnStartup:          app.startup,
		OnDomReady:         app.domReady,
		OnBeforeClose:      app.beforeClose,
		OnShutdown:         app.shutdown,
		Bind:               []any{app},
		SingleInstanceLock: singleInstanceLock(app),

		// Start hidden — domReady positions and shows the window after restoring
		// geometry, so the user never sees the default size/position flash.
		StartHidden: true,

		// Native application menu (File > Settings, Edit, Window).
		Menu: app.createAppMenu(),

		// Native OS file drops: the webview withholds dropped files' paths from the
		// HTML drop event, so the frontend (composer) reads them via runtime.OnFileDrop
		// against the --wails-drop-target element instead.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},

		// --- per-platform adaptation (see desktop/README.md for the rationale) ---
		Mac: &mac.Options{
			// Inset traffic-lights over a frameless-feeling header; the frontend
			// leaves a drag region at the top (CSS --wails-draggable).
			TitleBar: mac.TitleBarHiddenInset(),
			// Follow the OS appearance so the title bar matches light/dark system
			// preference instead of being locked to dark.
			Appearance: mac.DefaultAppearance,
		},
		Windows: &windows.Options{
			// Follow the OS theme so the title bar matches light/dark system
			// preference instead of being locked to dark.
			Theme:                windows.SystemDefault,
			WebviewGpuIsDisabled: windowsWebview2GPUDisabled(),
		},
		Linux: &linux.Options{
			ProgramName: product.Name,
			// WebKitGTK GPU compositing is inconsistent across distros/drivers and
			// is the one real cross-platform rough edge for a Go+webview stack:
			// "always" can yield blank or flickering webviews on some setups, so
			// we let the webview decide on demand. Users still hitting artifacts
			// can fall back to WEBKIT_DISABLE_COMPOSITING_MODE=1 (see README).
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

func addExecutableDirToPATH() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	if dir == "" || dir == "." {
		return
	}
	path := os.Getenv("PATH")
	for _, entry := range filepath.SplitList(path) {
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(dir)) {
			return
		}
	}
	if path == "" {
		_ = os.Setenv("PATH", dir)
		return
	}
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+path)
}
