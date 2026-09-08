package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/update"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool/hosttools"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type pendingDesktopUpdate struct {
	manifest update.Manifest
	filename string
	source   string
}

func (a *App) Version() string { return version }

func (a *App) CheckUpdate() (*UpdateInfo, error) {
	client, err := httpClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), 30*time.Second)
	defer cancel()
	manifest, err := fetchManifest(ctx, client)
	if err != nil {
		// Legacy releases may be linked, but never installed without a signed manifest.
		release, releaseErr := fetchLatestDesktopRelease(ctx, client)
		if releaseErr == nil {
			info := evaluateGitHubRelease(version, release)
			info.Err = err.Error()
			return &info, nil
		}
		return &UpdateInfo{Current: version, DownloadURL: ghReleasesBase, Err: err.Error()}, nil
	}
	info := evaluate(version, manifest)
	info.Source = manifest.Source
	a.updateMu.Lock()
	if a.updateCancel == nil && a.updateState.Phase != "ready" && !a.updateApplying.Load() {
		if info.Available {
			source := manifest.Source
			if source == "" && len(info.Sources) > 0 {
				source = info.Sources[0]
			}
			a.updatePending = &pendingDesktopUpdate{manifest: *manifest, source: source}
		} else {
			a.updatePending = nil
		}
	}
	a.updateURL = info.DownloadURL
	a.updateMu.Unlock()
	return &info, nil
}

func (a *App) OpenDownloadPage() {
	a.updateMu.RLock()
	address := a.updateURL
	a.updateMu.RUnlock()
	if address == "" {
		address = ghReleasesBase
	}
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, address)
	}
}

func (a *App) GetUpdateStatus() updateProgress {
	a.updateMu.RLock()
	defer a.updateMu.RUnlock()
	state := a.updateState
	if state.Phase == "" {
		state.Phase = "idle"
	}
	return state
}

// SetUpdateSource selects the next mirror for the pending signed manifest. The
// selection is runtime-only: credentials, proxy settings, and user config are
// never written as part of updater state.
func (a *App) SetUpdateSource(source string) error {
	source = strings.ToLower(strings.TrimSpace(source))
	if source != updateSourceMac && source != updateSourceGitHub {
		return errors.New("不支持的更新源")
	}
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	if a.updateCancel != nil || a.updateApplying.Load() {
		return errors.New("更新操作正在进行")
	}
	if a.updatePending == nil {
		return errors.New("请先检查可用更新")
	}
	asset, ok := selectedUpdateAsset(&a.updatePending.manifest)
	if !ok || !containsUpdateSource(updateAssetSources(asset), source) {
		return errors.New("当前更新没有此下载源")
	}
	a.updatePending.source = source
	a.updateState.Source = source
	a.updateState.SuggestAlternate = false
	return nil
}

func (a *App) DownloadUpdate() error {
	a.updateMu.Lock()
	if a.updateCancel != nil || a.updateApplying.Load() {
		a.updateMu.Unlock()
		return errors.New("更新操作正在进行")
	}
	pending := a.updatePending
	if pending == nil || !evaluate(version, &pending.manifest).Available {
		a.updateMu.Unlock()
		return errors.New("请先检查可用更新")
	}
	if a.updateState.Phase == "ready" && pending.filename != "" {
		a.updateMu.Unlock()
		return nil
	}
	asset, ok := selectedUpdateAsset(&pending.manifest)
	if !ok {
		a.updateMu.Unlock()
		return errors.New("没有适用于当前平台的安装包")
	}
	source := pending.source
	sources := updateAssetSources(asset)
	if len(sources) == 0 {
		a.updateMu.Unlock()
		return errors.New("没有可用的下载源")
	}
	if !containsUpdateSource(sources, source) {
		source = sources[0]
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), 2*time.Hour)
	a.updateCancel = cancel
	a.updateState = updateProgress{
		Phase:         "downloading",
		Version:       pending.manifest.Version,
		Source:        source,
		Sources:       sources,
		Total:         asset.Size,
		CanSelfUpdate: canSelfUpdate(),
	}
	a.updateMu.Unlock()
	go func() {
		defer cancel()
		client, err := httpClient()
		var filename string
		if err == nil {
			root, rootErr := os.UserCacheDir()
			err = rootErr
			if err == nil {
				dir := filepath.Join(root, "O.R.C.A", "updates", strings.ToLower(asset.SHA256))
				filename, err = downloadPackageWithSource(ctx, client, asset, dir, source, func(activeSource, phase string, stats packageTransferStats) {
					a.emitProgressWithStats(activeSource, phase, stats, "")
				}, update.VerifyReader)
			}
		}
		a.updateMu.Lock()
		a.updateCancel = nil
		if err == nil && ctx.Err() == nil {
			pending.filename = filename
			a.updateState.Phase = "ready"
		} else if ctx.Err() != nil {
			a.updateState.Phase = "cancelled"
		} else {
			a.updateState.Phase = "error"
			a.updateState.Err = err.Error()
		}
		state := a.updateState
		a.updateMu.Unlock()
		a.publishUpdateProgress(state)
	}()
	return nil
}

func (a *App) CancelUpdateDownload() error {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	if a.updateCancel != nil {
		a.updateCancel()
	}
	return nil
}

func (a *App) verifiedUpdate() (*pendingDesktopUpdate, error) {
	a.updateMu.RLock()
	if a.updatePending == nil || a.updateState.Phase != "ready" || a.updatePending.filename == "" {
		a.updateMu.RUnlock()
		return nil, errors.New("更新尚未下载并验证完成")
	}
	pending := *a.updatePending
	a.updateMu.RUnlock()
	if !evaluate(version, &pending.manifest).Available {
		return nil, errors.New("不能安装旧版本更新")
	}
	asset, ok := selectedUpdateAsset(&pending.manifest)
	if !ok {
		return nil, errors.New("更新平台不匹配")
	}
	sig, err := os.ReadFile(pending.filename + ".minisig")
	if err == nil {
		err = verifyPackage(pending.filename, asset, sig, update.VerifyReader)
	}
	if err != nil {
		return nil, err
	}
	return &pending, nil
}

func (a *App) ApplyUpdate() error {
	if !canSelfUpdate() {
		return errors.New("此安装方式需要打开下载的安装包完成更新")
	}
	if a.ctx == nil {
		return errors.New("应用窗口尚未就绪")
	}
	finish, err := a.acquireUpdateWorkBarrier()
	if err != nil {
		return err
	}
	success := false
	defer func() { finish(success) }()
	pending, err := a.verifiedUpdate()
	if err != nil {
		return err
	}
	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, tab := range a.tabs {
		tabs = append(tabs, tab)
	}
	a.mu.RUnlock()
	for _, tab := range tabs {
		if tab.Ctrl != nil {
			if err := tab.Ctrl.Snapshot(); err != nil {
				return fmt.Errorf("保存会话失败: %w", err)
			}
		}
	}
	handle, err := lockUpdatePackage(pending)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := installerCommand(pending.filename, currentInstallDir()).Start(); err != nil {
		return err
	}
	success = true
	a.emitProgress("applying", 0, 0, "")
	a.quitApp()
	return nil
}

func (a *App) OpenDownloadedUpdate() error {
	pending, err := a.verifiedUpdate()
	if err != nil {
		return err
	}
	if a.ctx == nil {
		return errors.New("应用窗口尚未就绪")
	}
	// Reveal the package directory; portable installs must not silently change install method.
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Dir(pending.filename))}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	wruntime.BrowserOpenURL(a.ctx, u.String())
	return nil
}

func (a *App) updateIdleCheck() error {
	if a.admittedWork.Load() != 0 {
		return errors.New("请等待已准入工作完成后再安装")
	}
	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab.runtimeReconfiguring || len(tab.pendingRuntimeSubmits) > 0 {
			a.mu.RUnlock()
			return errors.New("请等待排队任务和运行时切换完成后再安装")
		}
		tabs = append(tabs, tab)
	}
	a.mu.RUnlock()
	for _, tab := range tabs {
		if tab.Ctrl != nil && (tab.Ctrl.Running() || len(tab.Ctrl.Jobs()) > 0) {
			return errors.New("请等待当前回合和后台任务结束后再安装")
		}
	}
	a.sideChatMu.Lock()
	sideBusy := len(a.sideChatCancels) > 0
	a.sideChatMu.Unlock()
	if sideBusy {
		return errors.New("请等待侧边对话结束后再安装")
	}
	for _, job := range hosttools.ListAutomations() {
		if job.Status == "running" {
			return errors.New("请等待自动化任务结束后再安装")
		}
	}
	if a.computerUse != nil {
		state := a.computerUse.Current().State
		if state == computeruse.StateRunning || state == computeruse.StatePaused || state == computeruse.StateStopping {
			return errors.New("请结束电脑控制会话后再安装")
		}
	}
	return nil
}

func (a *App) reqCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) emitProgress(phase string, received, total int64, errMsg string) {
	a.updateMu.RLock()
	source := a.updateState.Source
	a.updateMu.RUnlock()
	a.emitProgressWithStats(source, phase, packageTransferStats{Received: received, Total: total}, errMsg)
}

func (a *App) emitProgressWithStats(source, phase string, stats packageTransferStats, errMsg string) {
	a.updateMu.Lock()
	a.updateState.Phase = phase
	if source != "" {
		a.updateState.Source = source
	}
	a.updateState.Received = stats.Received
	a.updateState.Total = stats.Total
	a.updateState.SpeedBPS = stats.SpeedBPS
	a.updateState.ETASeconds = stats.ETASeconds
	a.updateState.SuggestAlternate = stats.SuggestAlternate
	a.updateState.Err = errMsg
	state := a.updateState
	a.updateMu.Unlock()
	a.publishUpdateProgress(state)
}

func containsUpdateSource(sources []string, want string) bool {
	for _, source := range sources {
		if source == want {
			return true
		}
	}
	return false
}

func (a *App) publishUpdateProgress(state updateProgress) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "updater:progress", state)
	}
}
