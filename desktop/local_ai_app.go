package main

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/localai"
)

const (
	localDownloadEvent = "local:download-progress"
	localRuntimeEvent  = "local:runtime-status"
	localModelLoad     = "local:model-load-progress"
)

type LocalAICatalogView struct {
	Supported       bool                         `json:"supported"`
	Platform        string                       `json:"platform"`
	Models          []localai.ModelSpec          `json:"models"`
	Runtimes        []localai.RuntimeSpec        `json:"runtimes"`
	InstalledModels []localai.ModelInstallation  `json:"installedModels"`
	Runtime         *localai.RuntimeInstallation `json:"runtime,omitempty"`
	Downloads       []localai.DownloadTask       `json:"downloads"`
	Status          localai.RuntimeStatus        `json:"status"`
	Hardware        localai.HardwareProfile      `json:"hardware"`
	ModelsDirectory string                       `json:"modelsDirectory"`
}

func (a *App) localAIManager() *localai.Manager {
	a.localAIMu.Lock()
	defer a.localAIMu.Unlock()
	if a.localAI != nil {
		return a.localAI
	}
	modelsDir := ""
	if cfg, err := config.Load(); err == nil {
		modelsDir = strings.TrimSpace(cfg.LocalAI.ModelsDir)
	}
	a.localAI = localai.NewManagerWithModels(localai.DefaultRoot(), modelsDir, a.onLocalDownloadTask)
	return a.localAI
}

func (a *App) onLocalDownloadTask(task localai.DownloadTask) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, localDownloadEvent, task)
	}
	if task.State == localai.TaskCompleted {
		_ = a.registerInstalledLocalModels()
	}
}

func (a *App) emitLocalRuntimeStatus(status localai.RuntimeStatus) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, localRuntimeEvent, status)
		if status.State == localai.RuntimeLoading || status.State == localai.RuntimeRunning || status.State == localai.RuntimeFailed {
			wruntime.EventsEmit(a.ctx, localModelLoad, status)
		}
	}
}

func (a *App) GetHardwareProfile() localai.HardwareProfile {
	return localai.DetectHardware(a.localAIManager().Root())
}

func (a *App) GetLocalAICatalog() LocalAICatalogView {
	manager := a.localAIManager()
	hardware := localai.DetectHardware(manager.Root())
	runtimeInstall, ok := manager.InstalledRuntime()
	var runtimePtr *localai.RuntimeInstallation
	if ok {
		runtimeCopy := runtimeInstall
		runtimePtr = &runtimeCopy
	}
	modelsDir := localai.DefaultModelsDir()
	if cfg, err := config.Load(); err == nil && strings.TrimSpace(cfg.LocalAI.ModelsDir) != "" {
		modelsDir = cfg.LocalAI.ModelsDir
	}
	return normalizeLocalAICatalogView(LocalAICatalogView{
		Supported: runtime.GOOS == "windows", Platform: runtime.GOOS,
		Models: localai.ModelCatalog(), Runtimes: localai.RuntimeCatalog(),
		InstalledModels: manager.InstalledModels(), Runtime: runtimePtr,
		Downloads: manager.Tasks(), Status: a.localServer.Status(), Hardware: hardware,
		ModelsDirectory: modelsDir,
	})
}

func normalizeLocalAICatalogView(view LocalAICatalogView) LocalAICatalogView {
	if view.Models == nil {
		view.Models = []localai.ModelSpec{}
	}
	if view.Runtimes == nil {
		view.Runtimes = []localai.RuntimeSpec{}
	}
	if view.InstalledModels == nil {
		view.InstalledModels = []localai.ModelInstallation{}
	}
	if view.Downloads == nil {
		view.Downloads = []localai.DownloadTask{}
	}
	if view.Hardware.GPUs == nil {
		view.Hardware.GPUs = []localai.GPUAdapter{}
	}
	return view
}

func (a *App) StartLocalRuntimeInstall(runtimeID string) (localai.DownloadTask, error) {
	if runtime.GOOS != "windows" {
		return localai.DownloadTask{}, fmt.Errorf("本地推理首版仅支持 Windows")
	}
	if strings.TrimSpace(runtimeID) == "" {
		runtimeID = a.GetHardwareProfile().RecommendedRuntime
	}
	return a.localAIManager().StartRuntimeInstall(runtimeID)
}

func (a *App) StartLocalModelDownload(modelID string) (localai.DownloadTask, error) {
	if runtime.GOOS != "windows" {
		return localai.DownloadTask{}, fmt.Errorf("本地推理首版仅支持 Windows")
	}
	return a.localAIManager().StartModelDownload(strings.TrimSpace(modelID))
}

func (a *App) PauseLocalDownload(taskID string) error  { return a.localAIManager().Pause(taskID) }
func (a *App) ResumeLocalDownload(taskID string) error { return a.localAIManager().Resume(taskID) }
func (a *App) CancelLocalDownload(taskID string) error { return a.localAIManager().Cancel(taskID) }

func (a *App) StopLocalRuntime() error {
	if a.localServer == nil {
		return nil
	}
	return a.localServer.Stop()
}

func (a *App) UninstallLocalRuntime() error {
	if err := a.StopLocalRuntime(); err != nil {
		return err
	}
	return a.localAIManager().UninstallRuntime()
}

func (a *App) DeleteLocalModel(modelID string) error {
	status := a.localServer.Status()
	if status.ModelID == strings.TrimSpace(modelID) && (status.State == localai.RuntimeLoading || status.State == localai.RuntimeRunning) {
		if err := a.localServer.Stop(); err != nil {
			return err
		}
	}
	if err := a.ensureLocalModelNotReferenced(modelID); err != nil {
		return err
	}
	if err := a.localAIManager().DeleteModel(strings.TrimSpace(modelID)); err != nil {
		return err
	}
	return a.registerInstalledLocalModels()
}

func (a *App) ensureLocalModelNotReferenced(modelID string) error {
	ref := localai.ProviderID + "/" + strings.TrimSpace(modelID)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var roles []string
	if cfg.DefaultModel == ref {
		roles = append(roles, "默认模型")
	}
	if cfg.Agent.PlannerModel == ref {
		roles = append(roles, "planner")
	}
	if cfg.Agent.SubagentModel == ref {
		roles = append(roles, "subagent")
	}
	if cfg.Bot.Model == ref {
		roles = append(roles, "Orca")
	}
	if cfg.Desktop.ComputerControlModel == ref {
		roles = append(roles, "电脑控制")
	}
	if len(roles) > 0 {
		return fmt.Errorf("该模型仍被 %s 使用，请先选择替代模型", strings.Join(roles, "、"))
	}
	return nil
}

func (a *App) SetLocalModelsDirectory(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = localai.DefaultModelsDir()
	}
	a.localAIMu.Lock()
	manager := a.localAI
	a.localAIMu.Unlock()
	if manager != nil && len(manager.Tasks()) > 0 {
		for _, task := range manager.Tasks() {
			if task.State == localai.TaskDownloading || task.State == localai.TaskQueued || task.State == localai.TaskVerifying || task.State == localai.TaskInstalling {
				return fmt.Errorf("请等待当前下载或安装结束后再更改模型目录")
			}
		}
	}
	if err := a.applyConfigOnly(func(c *config.Config) error { c.LocalAI.ModelsDir = path; return nil }); err != nil {
		return err
	}
	a.localAIMu.Lock()
	a.localAI = localai.NewManagerWithModels(localai.DefaultRoot(), path, a.onLocalDownloadTask)
	a.localAIMu.Unlock()
	return nil
}

func (a *App) localRuntimeInstalled() bool {
	_, ok := a.localAIManager().InstalledRuntime()
	return ok
}

func (a *App) registerInstalledLocalModels() error {
	models := a.localAIManager().InstalledModels()
	return a.applyConfigOnly(func(c *config.Config) error {
		if len(models) == 0 {
			if p, ok := c.Provider(localai.ProviderID); ok {
				p.Model, p.Models, p.Default = "", nil, ""
			}
			return nil
		}
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		sort.Strings(ids)
		entry := config.ProviderEntry{Name: localai.ProviderID, Kind: "openai", BaseURL: localai.ProviderBase, Models: ids, Default: ids[0], APIKeyEnv: localai.ProviderKeyEnv, ContextWindow: 25_600, NoProxy: true}
		if len(ids) == 1 {
			entry.Model, entry.Models = ids[0], nil
		}
		if err := c.UpsertProvider(entry); err != nil {
			return err
		}
		addProviderAccess(c, localai.ProviderID)
		if strings.TrimSpace(c.DefaultModel) == "" {
			c.DefaultModel = localai.ProviderID + "/" + ids[0]
		}
		c.LocalAI.Enabled = true
		if c.LocalAI.ModelsDir == "" {
			c.LocalAI.ModelsDir = localai.DefaultModelsDir()
		}
		return nil
	})
}

func (a *App) prepareLocalRuntimeProviders(ctx context.Context, cfg *config.Config, primaryModel string) ([]config.ProviderEntry, error) {
	refs := []string{primaryModel, cfg.Agent.PlannerModel, cfg.Agent.SubagentModel}
	for _, ref := range cfg.Agent.SubagentModels {
		refs = append(refs, ref)
	}
	localIDs := map[string]bool{}
	for _, ref := range refs {
		providerID, modelID, ok := strings.Cut(strings.TrimSpace(ref), "/")
		if ok && providerID == localai.ProviderID && modelID != "" {
			localIDs[modelID] = true
		}
	}
	if len(localIDs) == 0 {
		return nil, nil
	}
	if len(localIDs) > 1 {
		return nil, fmt.Errorf("一个 Controller 不能同时常驻多个本地大模型；请将主模型、planner 和 subagent 设为同一本地模型")
	}
	modelID := ""
	for id := range localIDs {
		modelID = id
	}
	manager := a.localAIManager()
	runtimeInstall, ok := manager.InstalledRuntime()
	if !ok {
		return nil, fmt.Errorf("本地 llama.cpp 运行器尚未安装")
	}
	var installation localai.ModelInstallation
	for _, model := range manager.InstalledModels() {
		if model.ID == modelID {
			installation = model
			break
		}
	}
	if installation.ID == "" {
		return nil, fmt.Errorf("本地模型 %q 尚未下载或校验失败", modelID)
	}
	spec, ok := localai.ModelByID(modelID)
	if !ok {
		return nil, fmt.Errorf("本地模型 %q 不在签名目录中", modelID)
	}
	idleMinutes := cfg.LocalAI.IdleUnloadMinutes
	if idleMinutes < 0 {
		idleMinutes = 0
	}
	status, err := a.localServer.Start(ctx, runtimeInstall, installation, spec, localai.DetectHardware(manager.Root()), int64(cfg.LocalAI.VRAMReserveMiB), time.Duration(idleMinutes)*time.Minute)
	if err != nil {
		return nil, err
	}
	entry := localRuntimeProviderEntry(modelID, status)
	return []config.ProviderEntry{entry}, nil
}

func localRuntimeProviderEntry(modelID string, status localai.RuntimeStatus) config.ProviderEntry {
	entry := config.ProviderEntry{Name: localai.ProviderID, Kind: "openai", BaseURL: status.BaseURL, Model: modelID, ContextWindow: status.Profile.ContextSize, ModelContextWindows: map[string]int{modelID: status.Profile.ContextSize}, NoProxy: true}
	entry.WithAPIKey(status.APIKey)
	return entry
}

func (a *App) buildController(ctx context.Context, opts boot.Options, ownerTabID ...string) (*control.Controller, error) {
	cfg, err := config.LoadForRoot(opts.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	providers, err := a.prepareLocalRuntimeProviders(ctx, cfg, model)
	if err != nil {
		return nil, err
	}
	opts.RuntimeProviders = append(opts.RuntimeProviders, providers...)
	if runtime.GOOS == "windows" && len(ownerTabID) > 0 && strings.TrimSpace(ownerTabID[0]) != "" {
		opts.ExtraTools = append(opts.ExtraTools, a.computerTools(ownerTabID[0])...)
	}
	ctrl, err := boot.Build(ctx, opts)
	if err != nil {
		return nil, err
	}
	ctrl.SetTrustedComputerUseAccess(cfg.Desktop.ComputerUseFullAccess && cfg.Desktop.ComputerUseConsent == computerUseConsentVersion)
	return ctrl, nil
}

func (a *App) SetComputerControlModel(modelRef string) error {
	modelRef = strings.TrimSpace(modelRef)
	return a.applyConfigOnly(func(c *config.Config) error {
		if modelRef != "" {
			entry, ok := c.ResolveModel(modelRef)
			if !ok {
				return fmt.Errorf("unknown computer control model %q", modelRef)
			}
			modelRef = entry.Name + "/" + entry.Model
		}
		c.Desktop.ComputerControlModel = modelRef
		return nil
	})
}
