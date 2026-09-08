// bridge is the single seam between the React app and the Go kernel. In the Wails
// shell it calls the bound App methods (window.go.main.App.*) and subscribes to
// the runtime event stream (window.runtime.EventsOn). In a plain browser (`pnpm
// dev` outside the shell) those globals are absent, so it falls back to a mock
// that streams a canned turn through the same contract — letting the whole UI be
// developed and laid out without rebuilding the Go side.

import type * as GeneratedApp from "../../wailsjs/go/main/App";

import { t } from "./i18n";
import { modeWithAutoApproveTools, modeWithPlan, normalizeCollaborationMode, normalizeMode, normalizeToolApprovalMode } from "./types";

import type {
  BalanceInfo,
  BotConnectionView,
  BotConnectionDiagnostic,
  BotRuntimeStatusView,
  BotInstallPollResult,
  BotInstallStartResult,
  BotSettingsView,
  CapabilitiesView,
  AutomationView,
  AssistantMemorySettings,
  CancelAck,
  CheckpointMeta,
  CommandInfo,
  ContextInfo,
  ContextPanelInfo,
  DirEntry,
  DroppedItem,
  EffortInfo,
  FilePreview,
  HistoryMessage,
	HardwareProfile,
  JobView,
  MCPServerInput,
  MemoryView,
  Meta,
  ModelInfo,
	LocalAICatalogView,
	LocalDownloadTask,
	ComputerUseState,
	ComputerUseSession,
  NetworkView,
  ProjectNode,
  ProviderView,
  ProductCapabilities,
  PromptMode,
  RuntimeSwitchProgress,
  QuestionAnswer,
  RuntimeSwitchResult,
  ServerView,
  SessionMeta,
  SettingsView,
  SideChatMessage,
  SkillRootView,
  SkillView,
  SlashArgsResult,
  TabMeta,
  ToolLibrarySettings,
  TopicMeta,
  UpdateInfo,
  UpdateProgress,
  WireEvent,
  VisionCapability,
  WorkspaceChangesView,
  GitCommitView,
  GitCommitDetailView,
  WorkspaceView,
} from "./types";

const GLOBAL_PROJECT_ORDER_KEY = "__global__";

// AppBindings is derived from the Wails-generated Go → TS method signatures, so
// the compiler catches drift between the Go binding surface and the frontend mock.
// Run `wails generate module` after adding/renaming a bound method on App, then
// `pnpm typecheck` to verify the mock still satisfies the contract.
//
// Types for the new native-feel bindings — kept inline since they are
// bridge-specific and only used in AppBindings / the dev mock.
interface NativeConfirmRequest {
  title: string;
  message: string;
  detail: string;
  confirmLabel: string;
  cancelLabel: string;
  destructive: boolean;
}

interface DesktopWindowState {
  width: number;
  height: number;
  x: number;
  y: number;
  maximised: boolean;
}

// AppBindings is the hand-written contract between the React app and the Go
// kernel. It uses local types (types.ts) so components don't import generated
// model classes. _CheckGeneratedBindings catches drift: when a Go method is
// added or renamed, the generated types shift, and a key present in GeneratedApp
// but missing from AppBindings causes a type error here. Fix: add the new method
// to AppBindings, then run `pnpm typecheck` to verify.
export interface AppBindings {
  Platform(): Promise<string>;
  GetProductCapabilities(): Promise<ProductCapabilities>;
  Submit(input: string): Promise<void>;
  SubmitToTab(tabID: string, input: string): Promise<void>;
  SubmitDisplay(display: string, input: string): Promise<void>;
  SubmitDisplayToTab(tabID: string, display: string, input: string): Promise<void>;
  RunShell(command: string): Promise<void>;
  RunShellForTab(tabID: string, command: string): Promise<void>;
  Steer(text: string): Promise<void>;
  SteerForTab(tabID: string, text: string): Promise<void>;
  Cancel(): Promise<void>;
  CancelTab(tabID: string): Promise<void>;
  RequestCancelTab(tabID: string): Promise<CancelAck>;
  PauseTab(tabID: string): Promise<void>;
  ResumeTab(tabID: string): Promise<void>;
  Approve(id: string, allow: boolean, session: boolean, persist: boolean): Promise<void>;
  ApproveTab(tabID: string, id: string, allow: boolean, session: boolean, persist: boolean): Promise<void>;
  AnswerQuestion(id: string, answers: QuestionAnswer[]): Promise<void>;
  AnswerQuestionForTab(tabID: string, id: string, answers: QuestionAnswer[]): Promise<void>;
  ReplayPendingPrompts(): Promise<void>;
  SetPlanMode(on: boolean): Promise<void>;
  SetMode(mode: string): Promise<void>;
  SetModeForTab(tabID: string, mode: string): Promise<void>;
  SetAutoApproveTools(on: boolean): Promise<void>;
  SetCollaborationMode(mode: string): Promise<void>;
  SetCollaborationModeForTab(tabID: string, mode: string): Promise<void>;
  SetToolApprovalMode(mode: string): Promise<void>;
  SetToolApprovalModeForTab(tabID: string, mode: string): Promise<void>;
  SetAskWorkflowForTab(tabID: string, enabled: boolean): Promise<void>;
  SetStepThinkingForTab(tabID: string, enabled: boolean): Promise<void>;
  SetPromptModeForTab(tabID: string, mode: string): Promise<void>;
	SetConversationModeForTab(tabID: string, mode: string): Promise<void>;
	RequestConversationModeForTab(tabID: string, mode: string): Promise<RuntimeSwitchResult>;
  SetEnhancedModeForTab(tabID: string, enabled: boolean): Promise<void>;
  SetGoal(goal: string): Promise<void>;
  SetGoalForTab(tabID: string, goal: string): Promise<void>;
  ClearGoal(): Promise<void>;
  ClearGoalForTab(tabID: string): Promise<void>;
  Compact(): Promise<void>;
  NewSession(): Promise<void>;
  ClearSession(): Promise<void>;
  History(): Promise<HistoryMessage[]>;
  HistoryForTab(tabID: string): Promise<HistoryMessage[]>;
  Checkpoints(): Promise<CheckpointMeta[]>;
  CheckpointsForTab(tabID: string): Promise<CheckpointMeta[]>;
  Rewind(turn: number, scope: string): Promise<void>;
  Fork(turn: number): Promise<TabMeta>;
  SummarizeFrom(turn: number): Promise<void>;
  SummarizeUpTo(turn: number): Promise<void>;
  ListSessions(): Promise<SessionMeta[]>;
  ListTrashedSessions(): Promise<SessionMeta[]>;
  ResumeSession(path: string): Promise<HistoryMessage[]>;
  ResumeSessionForTab(tabID: string, path: string): Promise<HistoryMessage[]>;
  PreviewSession(path: string): Promise<HistoryMessage[]>;
  DeleteSession(path: string): Promise<void>;
  RestoreSession(path: string): Promise<void>;
  PurgeTrashedSession(path: string): Promise<void>;
  RenameSession(path: string, title: string): Promise<void>;
  ListWorkspaces(): Promise<WorkspaceView[]>;
  PickWorkspace(): Promise<string>;
  SwitchWorkspace(path: string): Promise<string>;
  RemoveWorkspace(path: string): Promise<void>;
  ContextUsage(): Promise<ContextInfo>;
  ContextUsageForTab(tabID: string): Promise<ContextInfo>;
  Balance(): Promise<BalanceInfo>;
  BalanceForTab(tabID: string): Promise<BalanceInfo>;
  Jobs(): Promise<JobView[]>;
  JobsForTab(tabID: string): Promise<JobView[]>;
  Meta(): Promise<Meta>;
  MetaForTab(tabID: string): Promise<Meta>;
  Commands(): Promise<CommandInfo[]>;
  Capabilities(): Promise<CapabilitiesView>;
  AddMCPServer(input: MCPServerInput): Promise<number>;
  UpdateMCPServer(name: string, input: MCPServerInput): Promise<void>;
  RemoveMCPServer(name: string): Promise<void>;
  ReconnectMCPServer(name: string): Promise<void>;
  ClearMCPServerAuthentication(name: string): Promise<void>;
  PickSkillFolder(): Promise<string>;
  AddSkillPath(path: string): Promise<void>;
  RemoveSkillPath(path: string): Promise<void>;
  RefreshSkills(): Promise<void>;
  SetSkillEnabled(name: string, enabled: boolean): Promise<void>;
  SetMCPServerEnabled(name: string, enabled: boolean): Promise<void>;
  SetMCPServerTier(name: string, tier: string): Promise<void>;
  SlashArgs(input: string): Promise<SlashArgsResult>;
  ListDir(rel: string): Promise<DirEntry[]>;
  SearchFileRefs(query: string): Promise<DirEntry[]>;
  ReadFile(rel: string): Promise<FilePreview>;
  WorkspaceChanges(): Promise<WorkspaceChangesView>;
  GitBranches(): Promise<string[]>;
  GitCheckout(branch: string): Promise<void>;
  WorkspaceGitHistory(path: string): Promise<GitCommitView[]>;
  WorkspaceGitCommitDetail(hash: string, path: string): Promise<GitCommitDetailView>;
  OpenWorkspacePath(rel: string): Promise<void>;
  RevealWorkspacePath(rel: string): Promise<void>;
  RevealPath(path: string): Promise<void>;
  SavePastedImage(dataUrl: string): Promise<string>;
  SaveClipboardImage(): Promise<string>;
  ReadClipboardFilePaths(): Promise<string[]>;
  SavePastedFile(name: string, dataUrl: string): Promise<string>;
  PickExportFile(defaultFilename: string, mimeType: string): Promise<string>;
  SaveExportFile(path: string, payload: string, base64Encoded: boolean): Promise<void>;
  AttachDropped(path: string): Promise<DroppedItem>;
  AttachmentDataURL(path: string): Promise<string>;
  Models(): Promise<ModelInfo[]>;
  SetModel(name: string): Promise<void>;
  ModelsForTab(tabID: string): Promise<ModelInfo[]>;
  SetModelForTab(tabID: string, name: string): Promise<void>;
  Effort(): Promise<EffortInfo>;
  SetEffort(level: string): Promise<void>;
  EffortForTab(tabID: string): Promise<EffortInfo>;
  SetEffortForTab(tabID: string, level: string): Promise<void>;
  Memory(): Promise<MemoryView>;
  Remember(scope: string, note: string): Promise<string>;
  Forget(name: string): Promise<void>;
  SaveDoc(path: string, body: string): Promise<string>;
  GetAssistantMemorySettings(): Promise<AssistantMemorySettings>;
  SetAssistantMemorySettings(settings: AssistantMemorySettings): Promise<void>;
  ClearAssistantMemories(): Promise<void>;
  Settings(): Promise<SettingsView>;
  GetWelcomeSuggestions(): Promise<string[]>;
  SetDefaultModel(ref: string): Promise<void>;
  SetAutomationModel(ref: string): Promise<void>;
  SetAutomationFullAccess(enabled: boolean): Promise<void>;
  SetPlannerModel(ref: string): Promise<void>;
  SetSubagentModel(ref: string): Promise<void>;
  SetSubagentEffort(level: string): Promise<void>;
  SetVisionModel(ref: string): Promise<void>;
  SetAutoPlan(mode: string): Promise<void>;
  SaveProvider(p: ProviderView): Promise<void>;
  UpdateProviderModels(name: string, models: string[], defaultModel: string): Promise<void>;
  AddOfficialProviderAccess(kind: string, key: string): Promise<void>;
  FetchProviderModels(p: ProviderView): Promise<string[]>;
  DeleteProvider(name: string): Promise<void>;
  RemoveProviderAccess(name: string): Promise<void>;
  SetProviderKey(apiKeyEnv: string, value: string): Promise<void>;
  ClearProviderKey(apiKeyEnv: string): Promise<void>;
  SetPermissionMode(mode: string): Promise<void>;
  SetAutoReviewModel(ref: string): Promise<void>;
  AddPermissionRule(list: string, rule: string): Promise<void>;
  RemovePermissionRule(list: string, rule: string): Promise<void>;
  SetSandbox(bash: string, network: boolean, workspaceRoot: string, allowWrite: string[]): Promise<void>;
  SetNetwork(n: NetworkView): Promise<void>;
  SetBotSettings(b: BotSettingsView): Promise<void>;
  SetBotSecret(envName: string, value: string): Promise<void>;
  ClearBotSecret(envName: string): Promise<void>;
  ConnectQQBot(req: { appId: string; appSecret: string; environment: string }): Promise<BotConnectionView>;
  StartBotConnectionInstall(provider: string, domain: string): Promise<BotInstallStartResult>;
  PollBotConnectionInstall(installID: string): Promise<BotInstallPollResult>;
  DiagnoseBotConnection(id: string): Promise<BotConnectionDiagnostic>;
  TestBotConnection(id: string, target?: string): Promise<BotConnectionDiagnostic>;
  GetBotRuntimeStatus(): Promise<BotRuntimeStatusView>;
  SetCloseBehavior(mode: string): Promise<void>;
  SetDesktopLanguage(lang: string): Promise<void>;
  SetDesktopAppearance(theme: string, style: string): Promise<void>;
  SetDesktopUIStyle(style: string): Promise<void>;
  RestartApp(): Promise<void>;
  SetDesktopUIScale(scale: number): Promise<void>;
  SetDesktopCheckUpdates(enabled: boolean): Promise<void>;
  SetExpandThinking(on: boolean): Promise<void>;
  SetProcessDisplayMode(mode: string): Promise<void>;
  SetActivityIndicatorEnabled(enabled: boolean): Promise<void>;
  SetVisionEnabled(enabled: boolean): Promise<void>;
  SetVisionMode(mode: string): Promise<void>;
  GetVisionCapabilities(): Promise<VisionCapability[]>;
  ProbeModelVision(modelRef: string): Promise<VisionCapability>;
  SetVisionCapabilityOverride(modelRef: string, override: string): Promise<VisionCapability>;
  MigrateDesktopPreferences(language: string, theme: string, style: string): Promise<void>;
  SetAgentParams(temperature: number, maxSteps: number, plannerMaxSteps: number, softCompactRatio: number, compactRatio: number, compactForceRatio: number, systemPrompt: string): Promise<void>;
  SetTrayLocale(locale: "en" | "zh"): Promise<void>;
  // SetBypass is the legacy Wails name for YOLO/full-access tool auto-approval
  // (ask questions and plan approvals still wait; deny rules still apply).
  // Runtime-only.
  SetBypass(on: boolean): Promise<void>;
  Version(): Promise<string>;
  CheckUpdate(): Promise<UpdateInfo | null>;
  DownloadUpdate(): Promise<void>;
  SetUpdateSource(source: string): Promise<void>;
  CancelUpdateDownload(): Promise<void>;
  GetUpdateStatus(): Promise<UpdateProgress>;
  ApplyUpdate(): Promise<void>;
  OpenDownloadedUpdate(): Promise<void>;
  OpenDownloadPage(): Promise<void>;
  NeedsOnboarding(): Promise<boolean>;
	GetOnboardingState(): Promise<{ required: boolean; completed: boolean; hasCloudModel: boolean; hasLocalRuntime: boolean; platform: string; providers: ProviderView[] }>;
	CompleteOnboarding(): Promise<void>;
	ConnectProviderPreset(presetID: string, apiKey: string): Promise<string[]>;
  ConnectKey(apiKey: string): Promise<void>;
	GetHardwareProfile(): Promise<HardwareProfile>;
	GetLocalAICatalog(): Promise<LocalAICatalogView>;
	StartLocalRuntimeInstall(runtimeID: string): Promise<LocalDownloadTask>;
	StartLocalModelDownload(modelID: string): Promise<LocalDownloadTask>;
	PauseLocalDownload(taskID: string): Promise<void>;
	ResumeLocalDownload(taskID: string): Promise<void>;
	CancelLocalDownload(taskID: string): Promise<void>;
	StopLocalRuntime(): Promise<void>;
	UninstallLocalRuntime(): Promise<void>;
	DeleteLocalModel(modelID: string): Promise<void>;
	SetLocalModelsDirectory(path: string): Promise<void>;
	SetComputerControlModel(modelRef: string): Promise<void>;
	GetComputerUseState(): Promise<ComputerUseState>;
	StartComputerUseSession(request: { tabId?: string; goal: string; successCriteria?: string; restrictions?: string; modelRef?: string }): Promise<ComputerUseSession>;
	ObserveComputerUse(): Promise<any>;
	ExecuteComputerAction(action: any): Promise<any>;
	SetComputerUseFullAccess(enabled: boolean): Promise<void>;
	PauseComputerUse(): Promise<ComputerUseSession>;
	ResumeComputerUse(): Promise<ComputerUseSession>;
	StopComputerUse(): Promise<void>;
  ListTabs(): Promise<TabMeta[]>;
  OpenProjectTab(workspaceRoot: string, topicID: string): Promise<TabMeta>;
  OpenGlobalTab(topicID: string): Promise<TabMeta>;
  OpenAutomationTab(topicID: string): Promise<TabMeta>;
  EnsureBlankTab(scope: string, workspaceRoot: string): Promise<TabMeta>;
  SetActiveTab(tabID: string): Promise<void>;
  ReorderTabs(tabIDs: string[]): Promise<void>;
  CloseTab(tabID: string): Promise<void>;
  ListProjectTree(): Promise<ProjectNode[]>;
  RenameProject(workspaceRoot: string, title: string): Promise<void>;
  SetProjectColor(workspaceRoot: string, color: string): Promise<void>;
  SetTopicPinned(topicID: string, pinned: boolean): Promise<void>;
  ReorderProjects(workspaceRoots: string[]): Promise<void>;
  CreateTopic(scope: string, workspaceRoot: string, title: string): Promise<TopicMeta>;
  RenameTopic(topicID: string, title: string): Promise<void>;
  GenerateTopicTitle(scope: string, workspaceRoot: string, topicID: string): Promise<string>;
  DeleteTopic(topicID: string): Promise<void>;
  TrashTopic(topicID: string): Promise<void>;
  ContextPanel(tabID: string): Promise<ContextPanelInfo>;
  ListAutomations(): Promise<AutomationView[]>;
  PauseAutomation(id: string): Promise<void>;
  ResumeAutomation(id: string): Promise<void>;
  CancelAutomation(id: string): Promise<void>;
  ClearFinishedAutomations(): Promise<void>;
  GetToolLibrarySettings(): Promise<ToolLibrarySettings>;
  SetToolLibrarySettings(settings: ToolLibrarySettings): Promise<void>;
  ListSideChat(tabID: string): Promise<SideChatMessage[]>;
  SendSideChat(tabID: string, input: string): Promise<void>;
  ClearSideChat(tabID: string): Promise<void>;
  CancelSideChat(tabID: string): Promise<void>;
  // New native-feel bindings (added with the desktop native-feel plan).
  ConfirmAction(req: NativeConfirmRequest): Promise<boolean>;
  SaveWindowState(state: DesktopWindowState): Promise<void>;
}

// Compile-time drift check. Exclude<A, B> extracts keys in A that are missing
// from B. If that set is non-empty, AssertNever<non-never> fails with
// "Type 'X' does not satisfy the constraint 'never'".
// _CheckGenToApp errors mean a generated Go method has no TS counterpart.
// These compare method *names* only; full signature checking isn't possible here
// because local types (types.ts) use plain interfaces while generated types
// (models.ts) use classes with a convertValues prototype method. The structural
// mismatch would produce false positives. Method-arity and parameter-order drift
// are caught at the call sites by tsc when components invoke app.<method>(...).
type AssertNever<T extends never> = T;
export type _CheckGenToApp = AssertNever<Exclude<keyof typeof GeneratedApp, keyof AppBindings>>;

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): () => void;
  BrowserOpenURL(url: string): void;
  // Native OS file drop (desktop only); useDropTarget gates delivery to elements
  // carrying the --wails-drop-target CSS property. Absent in the browser dev mock.
  OnFileDrop?(cb: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean): void;
  OnFileDropOff?(): void;
}

declare global {
  interface Window {
    runtime?: WailsRuntime;
    go?: { main?: { App?: AppBindings } };
  }
}

// Must match desktop/app.go's eventChannel constant.
const EVENT_CHANNEL = "agent:event";
const RUNTIME_SWITCH_EVENT_CHANNEL = "runtime:switch-progress";
const UPDATER_PROGRESS_EVENT_CHANNEL = "updater:progress";

// Resolve the Wails binding at CALL time, not module-load time: in dev the Wails
// runtime can inject window.go AFTER this module first evaluates, so snapshotting
// once would pin the browser mock for the whole session (and show fake data — the
// dev mock's model list leaking into the real app was exactly this bug).
function realApp(): AppBindings | undefined {
  return typeof window !== "undefined" ? window.go?.main?.App : undefined;
}

let mockSingleton: AppBindings | null = null;
function browserMockUIStyle(): "modern" | "classic" {
	try {
		return localStorage.getItem("orca-ui-style") === "classic" ? "classic" : "modern";
	} catch {
		return "modern";
	}
}
function getMock(): AppBindings {
  if (!mockSingleton) mockSingleton = makeMockApp();
  return mockSingleton;
}

// onEvent subscribes to the agent's typed event stream; returns an unsubscribe.
export function onEvent(cb: (e: WireEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn(EVENT_CHANNEL, (payload) => cb(payload as WireEvent));
  }
  return mockSubscribe(cb);
}

export function onRuntimeSwitchProgress(cb: (progress: RuntimeSwitchProgress) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn(RUNTIME_SWITCH_EVENT_CHANNEL, (payload) => cb(payload as RuntimeSwitchProgress));
  }
  return () => {};
}

export function onUpdaterProgress(cb: (progress: UpdateProgress) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn(UPDATER_PROGRESS_EVENT_CHANNEL, (payload) => cb(payload as UpdateProgress));
  }
  return () => {};
}

export function onLocalAIChanged(cb: () => void): () => void {
	if (!window.runtime?.EventsOn) return () => {};
	const offDownload = window.runtime.EventsOn("local:download-progress", cb);
	const offRuntime = window.runtime.EventsOn("local:runtime-status", cb);
	const offLoad = window.runtime.EventsOn("local:model-load-progress", cb);
	return () => { offDownload(); offRuntime(); offLoad(); };
}

export function onComputerUseChanged(cb: (state: ComputerUseSession) => void): () => void {
	if (!window.runtime?.EventsOn) return () => {};
	return window.runtime.EventsOn("computer:session", (payload) => cb(payload as ComputerUseSession));
}


// onFilesDropped subscribes to native OS file drops landing on the composer (the
// --wails-drop-target element); the callback gets the dropped files' absolute
// paths. No-op in the browser dev mock, where the runtime is absent.
export function onFilesDropped(cb: (paths: string[]) => void): () => void {
  const rt = typeof window !== "undefined" ? window.runtime : undefined;
  if (!rt?.OnFileDrop) return () => {};

  // Wails' internal ResolveFilePaths throws when a non-file object (e.g. the
  // window icon) is dragged onto the webview. The error is uncaught and crashes
  // the app. Intercept it here so only real file drops reach the callback.
  const suppressNonFileDragError = (e: ErrorEvent) => {
    if (e.message?.includes("additional File object is not a file on the disk")) {
      e.preventDefault();
    }
  };
  const suppressNonFileDragRejection = (e: PromiseRejectionEvent) => {
    const msg = e.reason?.message ?? String(e.reason);
    if (msg.includes("additional File object is not a file on the disk")) {
      e.preventDefault();
    }
  };
  window.addEventListener("error", suppressNonFileDragError);
  window.addEventListener("unhandledrejection", suppressNonFileDragRejection);

  rt.OnFileDrop((_x, _y, paths) => {
    if (Array.isArray(paths) && paths.length > 0) cb(paths);
  }, true);
  return () => {
    rt.OnFileDropOff?.();
    window.removeEventListener("error", suppressNonFileDragError);
    window.removeEventListener("unhandledrejection", suppressNonFileDragRejection);
  };
}

// onReady subscribes to the agent:ready event fired when boot.Build completes.
// The frontend re-fetches Meta/Context/History when this lands.
export function onReady(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("agent:ready", () => cb());
  }
  // In dev mock, fire immediately since there's no real boot sequence.
  cb();
  return () => {};
}

export function onProjectTreeChanged(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("project-tree:changed", () => cb());
  }
  return () => {};
}

export function onWelcomeSuggestions(cb: (prompts: string[]) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("welcome:suggestions", (payload) => cb(Array.isArray(payload) ? payload as string[] : []));
  }
  return () => {};
}

// app proxies each call to the live binding (or the dev mock only when truly
// outside the shell), so a late-injected window.go is picked up transparently.
export const app: AppBindings = new Proxy({} as AppBindings, {
  get(_t, prop) {
    const target = realApp() ?? getMock();
    const v = (target as unknown as Record<string, unknown>)[String(prop)];
    return typeof v === "function" ? (v as (...a: unknown[]) => unknown).bind(target) : v;
  },
});

// openExternal opens a URL in the system browser (so links in rendered markdown
// don't navigate the webview away from the app). Falls back to window.open in the
// browser dev mock.
export function openExternal(url: string): void {
  if (typeof window !== "undefined" && window.runtime?.BrowserOpenURL) {
    window.runtime.BrowserOpenURL(url);
  } else if (typeof window !== "undefined") {
    window.open(url, "_blank", "noopener");
  }
}

// --- browser dev mock --------------------------------------------------------

const listeners = new Set<(e: WireEvent) => void>();
let mockScopedTabId: string | undefined;

function mockSubscribe(cb: (e: WireEvent) => void): () => void {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

function emit(e: WireEvent) {
  const event = mockScopedTabId && !e.tabId ? { ...e, tabId: mockScopedTabId } : e;
  listeners.forEach((l) => l(event));
}

async function withMockTabScope<T>(tabId: string, fn: () => Promise<T>): Promise<T> {
  const previous = mockScopedTabId;
  mockScopedTabId = tabId || previous;
  try {
    return await fn();
  } finally {
    mockScopedTabId = previous;
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function baseName(path: string): string {
  return path.replace(/[/\\]+$/, "").split(/[/\\]/).filter(Boolean).pop() ?? path;
}

function browserPlatformOverride(): "darwin" | "windows" | "linux" | "" {
  if (typeof window === "undefined" || window.runtime) return "";
  const value = new URLSearchParams(window.location.search).get("platform");
  return value === "darwin" || value === "windows" || value === "linux" ? value : "";
}

function mockScenario(): "demo" | "fresh" | "running" {
  if (typeof window === "undefined") return "demo";
  const value = new URLSearchParams(window.location.search).get("mock")?.trim().toLowerCase();
  if (value === "fresh" || value === "empty" || value === "first-run") return "fresh";
  if (value === "running" || value === "busy" || value === "streaming") return "running";
  return "demo";
}

function makeMockApp(): AppBindings {
  const scenario = mockScenario();
  const freshMock = scenario === "fresh";
  const runningMock = scenario === "running";
  let cancelled = false;
  let pendingAskPreview = false;
  let pendingApprovalPreview = false;
  const globalWorkspaceRoot = "~/Library/Application Support/orca/global-workspace";
  let cwd = freshMock ? globalWorkspaceRoot : "~/projects/joyquant-db"; // mutable so PickWorkspace is visible in dev
  let workspaces = freshMock ? [] : ["~/projects/joyquant-db", "~/projects/joyquant-sys", "~/projects/orca", "~/projects/blade"];
  let mockEffort = "auto";
  const day = 86_400_000;
  const t0 = Date.now();
  let mockAutomations: AutomationView[] = [
    {
      id: "automation-demo",
      label: "每日构建检查",
      kind: "daily",
      schedule: "daily 09:00",
      action: "notify",
      createdAt: new Date(Date.now() - day).toISOString(),
      nextRunAt: new Date(Date.now() + 3_600_000).toISOString(),
      status: "scheduled",
      result: "",
    },
  ];
  let mockToolLibrarySettings: ToolLibrarySettings = {
    threadManagementEnabled: true,
    webSearchEnabled: true,
    replRuntimeEnabled: true,
    documentToolsEnabled: true,
    hostSystemToolsEnabled: true,
    conversationSearchEnabled: true,
    proactiveToolUseEnabled: true,
  };
  let mockAssistantMemorySettings: AssistantMemorySettings = {
    assistantAutoMemoryEnabled: true,
    assistantMemoryRecallEnabled: true,
  };
  const mockSideChats: Record<string, SideChatMessage[]> = {};
  // Mutable so MCP add/remove/retry are observable in browser dev.
  let capServers: ServerView[] = [
    {
      name: "codegraph",
      transport: "stdio",
      status: "disabled",
      builtIn: true,
      configured: true,
      autoStart: false,
      tier: "background",
      tools: 0,
      prompts: 0,
      resources: 0,
      toolList: [
        { name: "search", description: "搜索工作区里的符号、文件和文本。" },
        { name: "context", description: "读取符号或文件附近的源码上下文。" },
        { name: "trace", description: "沿代码图追踪调用方和被调用方。" },
        { name: "node", description: "检查指定代码图节点。" },
      ],
    },
    { name: "github", transport: "stdio", status: "connected", configured: true, autoStart: true, tier: "background", command: "npx", args: ["-y", "@modelcontextprotocol/server-github"], tools: 12, prompts: 2, resources: 0 },
    {
      name: "linear",
      transport: "http",
      status: "initializing",
      configured: true,
      autoStart: true,
      tier: "background",
      url: "https://mcp.linear.app/mcp",
      authStatus: "possible",
      authUrl: "https://mcp.linear.app/mcp",
      tools: 8,
      prompts: 0,
      resources: 0,
      toolList: [
        { name: "list_issues", description: "列出并筛选 Linear 问题。" },
        { name: "get_issue", description: "按 id 或 key 获取 Linear 问题。" },
        { name: "create_issue", description: "创建 Linear 问题。" },
        { name: "update_issue", description: "更新状态、负责人、优先级或标签。" },
        { name: "list_projects", description: "列出 Linear 项目。" },
        { name: "get_project", description: "获取项目详情。" },
        { name: "list_teams", description: "列出 Linear 团队。" },
        { name: "search", description: "搜索 Linear 工作区对象。" },
      ],
    },
    { name: "figma", transport: "http", status: "failed", configured: true, autoStart: true, tier: "background", url: "https://mcp.figma.com/mcp", authStatus: "required", authUrl: "https://mcp.figma.com/mcp", tools: 0, prompts: 0, resources: 0, error: "connect: 401 unauthorized" },
  ];
  const capSkills: SkillView[] = [
    { name: "explore", description: "用隔离 subagent 调研代码库", scope: "builtin", runAs: "subagent", enabled: true },
    { name: "review", description: "审查暂存区 diff", scope: "project", runAs: "inline", enabled: false },
    { name: "init", description: "为仓库生成 ORCA.md", scope: "builtin", runAs: "inline", enabled: true },
  ];
  let capSkillRoots: SkillRootView[] = [
    { dir: "~/projects/orca/.orca/skills", scope: "project", priority: 1, status: "missing", configured: false, removable: true, skills: 0 },
    {
      dir: "~/my-skills",
      scope: "custom",
      priority: 5,
      status: "ok",
      configured: true,
      removable: true,
      skills: 1,
      skillItems: [{ name: "review", description: "审查暂存区 diff", scope: "custom", runAs: "inline" }],
    },
    {
      dir: "~/.orca/skills",
      scope: "global",
      priority: 6,
      status: "ok",
      configured: false,
      removable: true,
      skills: 2,
      skillItems: [
        { name: "explore", description: "用隔离 subagent 调研代码库", scope: "global", runAs: "subagent" },
        { name: "init", description: "Scaffold an ORCA.md for this repo", scope: "global", runAs: "inline" },
      ],
    },
  ];
  const mockSwitchWorkspace = async (path: string) => {
    cwd = path || "~";
    workspaces = [cwd, ...workspaces.filter((p) => p !== cwd)].slice(0, 12);
    if (!mockProjectTree.some((node) => node.kind === "project" && node.root === cwd)) {
      mockProjectTree.unshift({
        key: `project_${cwd}`,
        kind: "project",
        label: baseName(cwd),
        root: cwd,
        children: [],
      });
    }
    return cwd;
  };
  // Mutable so delete/rename are observable in browser dev.
  const sessions: SessionMeta[] = [
    { path: "/mock/sessions/a.jsonl", preview: "fix the login bug in auth.go", turns: 12, createdAt: t0 - 2 * day, lastActivityAt: t0 - 3_600_000, modTime: t0 - 3_600_000, current: true, open: true },
    { path: "/mock/sessions/b.jsonl", preview: "refactor the payment module", turns: 5, createdAt: t0 - 3 * day, lastActivityAt: t0 - 6 * 3_600_000, modTime: t0 - 6 * 3_600_000, current: false, open: true },
    { path: "/mock/sessions/c.jsonl", preview: "write the README and badges", turns: 8, createdAt: t0 - 4 * day, lastActivityAt: t0 - day - 3_600_000, modTime: t0 - day - 3_600_000, current: false, open: false },
    { path: "/mock/sessions/d.jsonl", preview: "explain the plugin host design", turns: 3, createdAt: t0 - 5 * day, lastActivityAt: t0 - 4 * day, modTime: t0 - 4 * day, current: false, open: false },
  ];
  const trashedSessions: SessionMeta[] = [
    {
      path: "/mock/sessions/.trash/trash-dev-standard.jsonl",
      title: t("mock.trashDevStandardTitle"),
      preview: t("mock.trashDevStandardPreview"),
      turns: 4,
      createdAt: t0 - 8 * day,
      lastActivityAt: t0 - 7 * day,
      modTime: t0 - 7 * day,
      deletedAt: t0 - 20 * 60_000,
      current: false,
      open: false,
      scope: "project",
      workspaceRoot: "~/projects/joyquant-db",
      topicId: "topic_dev_standard",
      topicTitle: t("mock.trashDevStandardTitle"),
    },
    {
      path: "/mock/sessions/.trash/trash-p3a-review.jsonl",
      title: t("mock.trashP3aTitle"),
      preview: t("mock.trashP3aPreview"),
      turns: 7,
      createdAt: t0 - 6 * day,
      lastActivityAt: t0 - 5 * day,
      modTime: t0 - 5 * day,
      deletedAt: t0 - 2 * 3_600_000,
      current: false,
      open: false,
      scope: "project",
      workspaceRoot: "~/projects/joyquant-sys",
      topicId: "topic_p3a_pd",
      topicTitle: t("mock.trashP3aTitle"),
    },
    {
      path: "/mock/sessions/.trash/trash-global-product.jsonl",
      title: t("mock.trashGlobalProductTitle"),
      preview: t("mock.trashGlobalProductPreview"),
      turns: 2,
      createdAt: t0 - 4 * day,
      lastActivityAt: t0 - 3 * day,
      modTime: t0 - 3 * day,
      deletedAt: t0 - day,
      current: false,
      open: false,
      scope: "global",
      topicId: "topic_product",
      topicTitle: t("mock.trashGlobalProductTitle"),
    },
  ];
  if (freshMock) {
    sessions.splice(0);
    trashedSessions.splice(0);
  }
  // Mutable settings so the Settings panel's edits are observable in browser dev.
  const settings: SettingsView = {
    defaultModel: "deepseek",
    automationModel: "deepseek/deepseek-v4-flash",
    plannerModel: "",
    subagentModel: "",
    subagentEffort: "",
    autoPlan: "off",
    providers: [
      { name: "deepseek", builtIn: true, added: false, kind: "openai", baseUrl: "https://api.deepseek.com", modelsUrl: "", models: ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"], default: "deepseek-v4-flash", apiKeyEnv: "DEEPSEEK_API_KEY", keySet: true, balanceUrl: "https://api.deepseek.com/user/balance", contextWindow: 1_000_000, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
      { name: "mimo-token-plan", builtIn: true, added: false, kind: "openai", baseUrl: "https://token-plan-cn.xiaomimimo.com/v1", modelsUrl: "", models: ["mimo-v2.5-pro"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_API_KEY", keySet: false, balanceUrl: "", contextWindow: 1_048_576, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
    ],
    officialProviders: [
      { name: "deepseek", builtIn: true, added: false, kind: "openai", baseUrl: "https://api.deepseek.com", modelsUrl: "", models: ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"], default: "deepseek-v4-flash", apiKeyEnv: "DEEPSEEK_API_KEY", keySet: true, balanceUrl: "https://api.deepseek.com/user/balance", contextWindow: 1_000_000, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
      { name: "mimo-api", builtIn: true, added: false, kind: "openai", baseUrl: "https://api.xiaomimimo.com/v1", modelsUrl: "", models: ["mimo-v2.5", "mimo-v2.5-pro"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_API_KEY", keySet: false, balanceUrl: "", contextWindow: 1_048_576, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
      { name: "mimo-token-plan", builtIn: true, added: false, kind: "openai", baseUrl: "https://token-plan-cn.xiaomimimo.com/v1", modelsUrl: "", models: ["mimo-v2.5-pro"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_API_KEY", keySet: false, balanceUrl: "", contextWindow: 1_048_576, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
    ],
    permissions: { mode: "ask", autoReviewModel: "", allow: ["ls", "read_file"], ask: [], deny: ["Bash(rm:*)"] },
    sandbox: { bash: "enforce", network: true, workspaceRoot: "", allowWrite: [] },
    network: {
      proxyMode: "auto",
      proxyUrl: "",
      noProxy: "",
      proxy: { type: "socks5", server: "127.0.0.1", port: 7890, username: "", password: "" },
    },
    agent: { temperature: 0.2, maxSteps: 0, plannerMaxSteps: 12, softCompactRatio: 0.5, compactRatio: 0.8, compactForceRatio: 0.9, systemPrompt: "你是 O.R.C.A.，一个专注于代码任务的智能编程 Agent。" },
    bot: {
      enabled: false,
      model: "",
	  promptMode: "assistant",
      workspaceRoot: "~/.config/orca/bot-workspace",
      maxSteps: 25,
      debounceMs: 1500,
      allowlist: {
        enabled: true,
        allowAll: false,
        qqUsers: [],
        feishuUsers: [],
        weixinUsers: [],
        qqGroups: [],
        feishuGroups: [],
        weixinGroups: [],
      },
      qq: { enabled: false, appId: "", appSecretEnv: "QQ_BOT_APP_SECRET", secretSet: false, environment: "sandbox" },
      feishu: {
        enabled: false,
        domain: "feishu",
        appId: "",
        appSecretEnv: "FEISHU_BOT_APP_SECRET",
        secretSet: false,
        verificationToken: "",
        mode: "webhook",
        webhookPort: 8080,
        requireMention: true,
      },
      weixin: {
        enabled: false,
        accountId: "default",
        tokenEnv: "WEIXIN_BOT_TOKEN",
        tokenSet: false,
        apiBase: "https://ilinkai.weixin.qq.com",
      },
      connections: [],
    },
    desktopLanguage: "",
    desktopTheme: "light",
    desktopThemeStyle: "slate",
	desktopUIStyle: browserMockUIStyle(),
    closeBehavior: "background",
    checkUpdates: true,
    expandThinking: false,
    processDisplayMode: "compact",
    activityIndicatorEnabled: true,
    visionEnabled: false,
    visionMode: "auto",
    uiScale: 0,
    effectiveUIScale: 100,
    automationFullAccessApproved: false,
	computerControlModel: "",
	computerUseFullAccessApproved: false,
    configPath: "~/projects/orca/orca.toml",
    providerKinds: ["openai"],
    autoApproveTools: false,
    bypass: false,
  };
  let mockVisionCapabilities: VisionCapability[] = settings.providers.flatMap((provider) => provider.models.map((model) => ({
    modelRef: `${provider.name}/${model}`,
    key: `${provider.kind}|${provider.baseUrl}|${model}`,
    status: provider.name === "deepseek" ? "unsupported" : "unknown",
    checkedAt: provider.name === "deepseek" ? Date.now() : undefined,
    reason: provider.name === "deepseek" ? "model does not support image input" : undefined,
  })));
  settings.providers = settings.providers.map((provider) =>
    provider.apiKeyEnv === "DEEPSEEK_API_KEY" ? { ...provider, keySet: !freshMock } : provider,
  );
  if (freshMock) {
    settings.configPath = "~/.config/orca/config.toml";
  }
  const mockNow = Date.now();
  const mockProjectTree: ProjectNode[] = freshMock ? [] : [
    {
      key: "project_~/projects/joyquant-db",
      kind: "project",
      label: t("mock.projectJoyquantDb"),
      root: "~/projects/joyquant-db",
      projectColor: "blue",
      children: [
        { key: "topic_dev_standard", kind: "topic", label: `● ${t("mock.topicDevStandard")}`, root: "~/projects/joyquant-db", topicId: "topic_dev_standard", projectColor: "blue", turns: 18, lastActivityAt: mockNow - 8 * 60_000, open: true, running: runningMock },
        { key: "topic_db_maint", kind: "topic", label: t("mock.topicDbMaint"), root: "~/projects/joyquant-db", topicId: "topic_db_maint", projectColor: "blue", turns: 7, lastActivityAt: mockNow - 2 * 60 * 60_000 },
        { key: "topic_env", kind: "topic", label: t("mock.topicEnv"), root: "~/projects/joyquant-db", topicId: "topic_env", projectColor: "blue", turns: 3, lastActivityAt: mockNow - 26 * 60 * 60_000 },
      ],
    },
    {
      key: "project_~/projects/joyquant-sys",
      kind: "project",
      label: t("mock.projectJoyquantSys"),
      root: "~/projects/joyquant-sys",
      projectColor: "purple",
      children: [
        { key: "topic_p3b_pd", kind: "topic", label: `● ${t("mock.topicP3b")}`, root: "~/projects/joyquant-sys", topicId: "topic_p3b_pd", projectColor: "purple", turns: 11, lastActivityAt: mockNow - 3 * 24 * 60 * 60_000, status: runningMock ? "streaming" : undefined },
        { key: "topic_p3a_pd", kind: "topic", label: t("mock.topicP3a"), root: "~/projects/joyquant-sys", topicId: "topic_p3a_pd", projectColor: "purple", turns: 9, lastActivityAt: mockNow - 4 * 24 * 60 * 60_000, status: runningMock ? "thinking" : undefined },
        { key: "topic_hotfix", kind: "topic", label: t("mock.topicHotfix"), root: "~/projects/joyquant-sys", topicId: "topic_hotfix", projectColor: "purple", turns: 4, lastActivityAt: mockNow - 5 * 24 * 60 * 60_000, status: runningMock ? "thinking" : undefined },
        { key: "topic_sys_coord", kind: "topic", label: t("mock.topicSysCoord"), root: "~/projects/joyquant-sys", topicId: "topic_sys_coord", projectColor: "purple", turns: 14, lastActivityAt: mockNow - 6 * 24 * 60 * 60_000, status: runningMock ? "waiting_confirmation" : undefined },
        { key: "topic_sys_standard", kind: "topic", label: t("mock.topicSysStandard"), root: "~/projects/joyquant-sys", topicId: "topic_sys_standard", projectColor: "purple", turns: 6, lastActivityAt: mockNow - 7 * 24 * 60 * 60_000, status: "paused" },
        { key: "topic_sys_exception", kind: "topic", label: t("mock.topicSysException"), root: "~/projects/joyquant-sys", topicId: "topic_sys_exception", projectColor: "purple", turns: 2, lastActivityAt: mockNow - 8 * 24 * 60 * 60_000, status: "error" },
      ],
    },
    {
      key: "global_folder",
      kind: "global_folder",
      label: "独立工作区",
      root: globalWorkspaceRoot,
      children: [
        { key: "global_topic_product", kind: "global_topic", label: t("mock.topicProduct"), topicId: "topic_product", turns: 5, lastActivityAt: mockNow - 8 * 24 * 60 * 60_000 },
        { key: "global_topic_ai", kind: "global_topic", label: t("mock.topicAi"), topicId: "topic_ai", turns: 8, lastActivityAt: mockNow - 10 * 24 * 60 * 60_000 },
        { key: "global_topic_lab", kind: "global_topic", label: t("mock.topicLab"), topicId: "topic_lab", turns: 2, lastActivityAt: mockNow - 12 * 24 * 60 * 60_000 },
      ],
    },
  ];
  const ensureMockGlobalFolder = (): ProjectNode => {
    let node = mockProjectTree.find((item) => item.kind === "global_folder");
    if (!node) {
      node = {
        key: "global_folder",
        kind: "global_folder",
        label: "独立工作区",
        root: globalWorkspaceRoot,
        children: [],
      };
      mockProjectTree.push(node);
    }
    return node;
  };
  const cloneProjectTree = () => {
    if (mockProjectTree.length === 0) ensureMockGlobalFolder();
    const tree = JSON.parse(JSON.stringify(mockProjectTree)) as ProjectNode[];
    const pinned = tree
      .flatMap((node) => projectChildren(node))
      .filter((node) => node.pinned)
      .sort((a, b) => (b.lastActivityAt ?? 0) - (a.lastActivityAt ?? 0))
      .map((node) => ({ ...node, key: `pinned_${node.key}`, children: undefined }));
    const pinnedFolder: ProjectNode = { key: "pinned_folder", kind: "pinned_folder", label: "置顶", children: pinned };
    return pinned.length > 0 ? [pinnedFolder, ...tree] : tree;
  };
  const projectChildren = (node: ProjectNode): ProjectNode[] => Array.isArray(node.children) ? node.children : [];
  const findMockTopic = (topicId: string): ProjectNode | null => {
    for (const parent of mockProjectTree) {
      const found = projectChildren(parent).find((child) => child.topicId === topicId);
      if (found) return found;
    }
    return null;
  };
  const deleteMockTopic = (topicId: string) => {
    for (const parent of mockProjectTree) {
      parent.children = projectChildren(parent).filter((child) => child.topicId !== topicId);
    }
  };
  const topicLabel = (topicId: string, fallback: string) => (findMockTopic(topicId)?.label || fallback).replace(/^●\s*/, "");
  const mockTopicStatus = (topicId: string) => findMockTopic(topicId)?.status ?? "";
  const mockTopicIsRunning = (topicId: string) => {
    const status = mockTopicStatus(topicId);
    return status === "streaming" || status === "thinking" || status === "waiting_confirmation";
  };
  const mockTopicRunsInScenario = (topicId: string) => runningMock && mockTopicIsRunning(topicId);
  const mockTopicHistory = (topicId: string): HistoryMessage[] => {
    switch (topicId) {
      case "topic_p3b_pd":
        return [
          { role: "user", content: "把 p3b P&D 的范围和风险重新整理成可执行计划。" },
          { role: "phase", content: "分析需求范围" },
        ];
      case "topic_p3a_pd":
        return [
          { role: "user", content: "复盘 p3a 的技术方案，先不要写文件，先说明你的判断。" },
        ];
      case "topic_hotfix":
        return [
          { role: "user", content: "检查 post-p3-hotfix 的回归风险，重点看最近的 shell 输出和 git 改动。" },
          { role: "assistant", content: "", reasoning: "我先定位最近一次 hotfix 的上下文，然后用只读命令检查状态；左侧保持“思考中”，工具细节在这里展开。" },
        ];
      case "topic_sys_coord":
        return [
          { role: "user", content: "准备执行 joyquant-sys 的同步脚本，但需要我确认后再运行。" },
          { role: "assistant", content: "", reasoning: "这个动作会运行脚本并可能刷新本地缓存，所以需要先等用户确认。" },
        ];
      case "topic_sys_standard":
        return [
          { role: "user", content: "继续制定 SYS 项目开发规范，先停在当前检查点。" },
          { role: "assistant", content: "已暂停在规范整理阶段。当前保留了目录约定、分支策略和待确认的发布检查项；继续时可以从这里恢复。" },
          { role: "notice", level: "info", content: "会话已暂停：未继续执行命令，等待用户恢复或切换任务。" },
        ];
      case "topic_sys_exception":
        return [
          { role: "user", content: "演练异常处理流程，看看失败时界面怎么提示。" },
          { role: "assistant", content: "我尝试校验恢复脚本时遇到异常，已停止继续执行。" },
          { role: "notice", level: "warn", content: "运行异常：恢复脚本缺少必要环境变量 JOYQUANT_SYS_TOKEN。请补齐配置后重试。" },
        ];
      default:
        return [];
    }
  };
  const mockRuntimeInjected = new Set<string>();
  const queueMockTopicRuntime = (tab: TabMeta) => {
    if (!runningMock) return;
    const status = mockTopicStatus(tab.topicId);
    if (status !== "streaming" && status !== "thinking" && status !== "waiting_confirmation") return;
    const key = `${tab.id}:${tab.topicId}:${status}`;
    if (mockRuntimeInjected.has(key)) return;
    mockRuntimeInjected.add(key);
    window.setTimeout(() => {
      void withMockTabScope(tab.id, async () => {
        emitMockTurnStarted();
        await delay(120);
        if (tab.topicId === "topic_p3b_pd") {
          const text = "我会先把范围拆成三层：目标、依赖、风险。当前已经确认 p3b 的交付边界，接下来补充每个模块的验收口径...";
          for (const ch of text) {
            emit({ kind: "text", text: ch });
            await delay(5);
          }
          return;
        }
        if (tab.topicId === "topic_p3a_pd") {
          emit({ kind: "reasoning", text: "我正在对比 p3a 和 p3b 的差异：先看约束，再看变更风险，最后判断是否需要拆成独立任务。\n\n" });
          await delay(220);
          emit({ kind: "reasoning", text: "当前倾向：先保留 p3a 的兼容路径，不急于删除旧逻辑。" });
          return;
        }
        if (tab.topicId === "topic_hotfix") {
          const id = "mock-hotfix-shell";
          emit({ kind: "tool_dispatch", tool: { id, name: "bash", args: JSON.stringify({ command: "git status --short && npm test" }), readOnly: true } });
          await delay(180);
          emit({ kind: "tool_progress", tool: { id, name: "bash", readOnly: true, output: "$ git status --short\n M internal/sys/runner.go\n\n$ npm test\nrunning targeted regression tests...\n" } });
          return;
        }
        if (tab.topicId === "topic_sys_coord") {
          pendingApprovalPreview = true;
          emit({ kind: "reasoning", text: "我已经准备好执行同步脚本，但这个操作会影响本地 workspace，需要用户确认。" });
          await delay(160);
          emit({
            kind: "approval_request",
            approval: {
              id: "mock-sys-confirm",
              tool: "bash",
              subject: "npm run sync:joyquant-sys\n\n该命令会同步 SYS 项目配置并刷新本地缓存。",
            },
          });
        }
      });
    }, 180);
  };
  const setMockActiveTab = (tabId: string) => {
    mockTabs = mockTabs.map((tab) => ({ ...tab, active: tab.id === tabId }));
  };
  const currentMockTurnTabId = () => mockScopedTabId || mockTabs.find((tab) => tab.active)?.id;
  const setMockTabRunning = (tabId: string | undefined, running: boolean) => {
    if (!tabId) return;
    mockTabs = mockTabs.map((tab) => (tab.id === tabId ? { ...tab, running } : tab));
  };
  const emitMockTurnStarted = () => {
    setMockTabRunning(currentMockTurnTabId(), true);
    emit({ kind: "turn_started" });
  };
  const emitMockTurnDone = () => {
    setMockTabRunning(currentMockTurnTabId(), false);
    emit({ kind: "turn_done" });
  };
  let mockTabs: TabMeta[] = freshMock ? [
    {
      id: "tab_global",
      scope: "global",
      workspaceRoot: globalWorkspaceRoot,
      workspaceName: "独立工作区",
      topicId: "",
      topicTitle: "独立工作区",
      label: "DeepSeek-R1",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      active: true,
      cwd: globalWorkspaceRoot,
    },
  ] : [
    {
      id: "tab_joyquant_db",
      scope: "project",
      workspaceRoot: "~/projects/joyquant-db",
      workspaceName: "joyquant-db",
      topicId: "topic_dev_standard",
      topicTitle: t("mock.trashDevStandardTitle"),
      projectColor: "blue",
      label: "DeepSeek-R1",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      active: true,
      cwd: "~/projects/joyquant-db",
    },
    {
      id: "tab_joyquant_sys",
      scope: "project",
      workspaceRoot: "~/projects/joyquant-sys",
      workspaceName: "joyquant-sys",
      topicId: "topic_p3b_pd",
      topicTitle: "p3b P&D",
      projectColor: "purple",
      label: "DeepSeek-R1",
      ready: true,
      running: runningMock && mockTopicIsRunning("topic_p3b_pd"),
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      active: false,
      cwd: "~/projects/joyquant-sys",
    },
    {
      id: "tab_global",
      scope: "global",
      workspaceRoot: "",
      workspaceName: "独立工作区",
      topicId: "topic_global",
      topicTitle: "独立工作区",
      label: "DeepSeek-R1",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      active: false,
      cwd: "~/projects/joyquant-db",
    },
  ];
  const mockModelCatalog = [
    { ref: "deepseek/deepseek-v4-flash", provider: "deepseek", model: "deepseek-v4-flash" },
    { ref: "deepseek/deepseek-v4-pro", provider: "deepseek", model: "deepseek-v4-pro" },
    { ref: "deepseek/deepseek-v4-flash-vision-exp", provider: "deepseek", model: "deepseek-v4-flash-vision-exp" },
  ];
  const defaultMockModelRef = mockModelCatalog[0].ref;
  const mockModelRef = (name: string): string => {
    const trimmed = name.trim();
    if (!trimmed || trimmed === "DeepSeek-R1") return defaultMockModelRef;
    const exact = mockModelCatalog.find((model) => model.ref === trimmed);
    if (exact) return exact.ref;
    const byModel = mockModelCatalog.find((model) => model.model === trimmed);
    return byModel?.ref ?? trimmed;
  };
  const mockModelLabel = (ref: string): string => mockModelCatalog.find((model) => model.ref === mockModelRef(ref))?.model ?? ref.split("/").pop() ?? ref;
  const mockTabModelRef = (tab?: TabMeta): string => mockModelRef(tab?.label ?? "");
  const setMockTabModel = (tabID: string | undefined, name: string) => {
    const ref = mockModelRef(name);
    const label = mockModelLabel(ref);
    let applied = false;
    mockTabs = mockTabs.map((tab) => {
      const match = tabID ? tab.id === tabID : tab.active;
      if (!match) return tab;
      applied = true;
      return { ...tab, label };
    });
    if (!applied && mockTabs.length > 0) {
      mockTabs = mockTabs.map((tab, index) => (index === 0 ? { ...tab, label } : tab));
    }
  };
  return {
    async Platform() {
      const override = browserPlatformOverride();
      if (override) return override;
      // Mirror the OS the browser dev mock runs on.
      const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
      if (/Win/i.test(ua)) return "windows";
      if (/Mac/i.test(ua)) return "darwin";
      return "linux";
    },
    async GetProductCapabilities() {
      return {
        edition: "engineering",
			promptModes: ["assistant", "coding"],
			conversationModes: ["assistant", "coding"],
		assistantMemoryEnabled: true,
		orcaEnabled: true,
      };
    },
        async Submit(input) {
          cancelled = false;
      emitMockTurnStarted();
      const trimmedInput = input.trim().toLowerCase();
      const goalMatch = /^\/goal(?:\s+([\s\S]*))?$/.exec(input.trim());
      if (goalMatch) {
        const arg = (goalMatch[1] ?? "").trim();
        const lowered = arg.toLowerCase();
        const active = mockTabs.find((tab) => tab.active);
        if (!arg || lowered === "status") {
          emit({ kind: "notice", level: "info", text: active?.goal ? `goal: ${active.goal}` : "goal: none" });
          emitMockTurnDone();
          return;
        }
        if (["clear", "off", "stop", "done"].includes(lowered)) {
          mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: "", goalStatus: "stopped", collaborationMode: "normal" } : tab));
          emit({ kind: "notice", level: "info", text: "goal cleared" });
          emitMockTurnDone();
          return;
        }
        mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: arg, goalStatus: "running", collaborationMode: "goal" } : tab));
        emit({ kind: "notice", level: "info", text: `goal set: ${arg}` });
        await delay(350);
        if (cancelled) return;
        const reply = `Autonomous goal run started for: **${arg}**\n\nMock run completed.`;
        emit({ kind: "message", text: reply });
        mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: "", goalStatus: "complete", collaborationMode: "normal" } : tab));
        emitMockTurnDone();
        return;
      }
      if (trimmedInput === "/approve-preview" || trimmedInput === "approve preview" || trimmedInput === "approve预览") {
        pendingApprovalPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-approval-preview",
            tool: "bash",
            subject: t("mock.approvalSubject"),
          },
        });
        return;
      }
      if (
        trimmedInput === "/plan-approve-preview" ||
        trimmedInput === "plan approve preview" ||
        trimmedInput === "plan approve预览"
      ) {
        pendingApprovalPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-plan-approval-preview",
            tool: "exit_plan_mode",
            subject: "",
          },
        });
        return;
      }
      if (trimmedInput === "/ask-preview" || trimmedInput === "ask preview" || trimmedInput === "ask预览") {
        pendingAskPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "ask_request",
          ask: {
            id: "mock-ask-preview",
            questions: [
              {
                id: "q1",
                header: t("mock.askQ1Header"),
                prompt: t("mock.askQ1Prompt"),
                options: [
                  { label: t("mock.askQ1Opt1Label"), description: t("mock.askQ1Opt1Desc") },
                  { label: t("mock.askQ1Opt2Label"), description: t("mock.askQ1Opt2Desc") },
                  { label: t("mock.askQ1Opt3Label"), description: t("mock.askQ1Opt3Desc") },
                ],
              },
              {
                id: "q2",
                header: t("mock.askQ2Header"),
                prompt: t("mock.askQ2Prompt"),
                options: [
                  { label: t("mock.askQ2Opt1Label"), description: t("mock.askQ2Opt1Desc") },
                  { label: t("mock.askQ2Opt2Label"), description: t("mock.askQ2Opt2Desc") },
                  { label: t("mock.askQ2Opt3Label"), description: t("mock.askQ2Opt3Desc") },
                ],
              },
            ],
          },
        });
        return;
      }
      if (trimmedInput === "/todo-preview" || trimmedInput === "todo preview" || trimmedInput === "todo预览") {
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "tool_dispatch",
          tool: {
            id: "mock-todo-preview",
            name: "todo_write",
            args: JSON.stringify({
              todos: [
                { content: t("mock.todo1"), status: "completed" },
                { content: t("mock.todo2"), activeForm: t("mock.todo2ActiveForm"), status: "in_progress" },
                { content: t("mock.todo3"), status: "pending" },
              ],
            }),
            readOnly: false,
          },
        });
        await delay(150);
        emit({
          kind: "tool_result",
          tool: {
            id: "mock-todo-preview",
            name: "todo_write",
            args: JSON.stringify({
              todos: [
                { content: t("mock.todo1"), status: "completed" },
                { content: t("mock.todo2"), activeForm: t("mock.todo2ActiveForm"), status: "in_progress" },
                { content: t("mock.todo3"), status: "pending" },
              ],
            }),
            output: "todo list updated",
            readOnly: false,
            durationMs: 150,
          },
        });
        emitMockTurnDone();
        return;
      }
      if (trimmedInput === "/process-preview" || trimmedInput === "process preview" || trimmedInput === "过程预览") {
        await delay(200);
        if (cancelled) return;
        emit({ kind: "phase", text: "Preparing context" });
        await delay(120);
        emit({ kind: "notice", level: "info", text: "Loaded project instructions from AGENTS.md." });
        await delay(120);
        emit({ kind: "notice", level: "warn", text: "Network access is enabled; external results may change over time." });
        await delay(120);
        emit({ kind: "compaction_started", compaction: { trigger: "manual" } });
        await delay(320);
        emit({
          kind: "compaction_done",
          compaction: {
            trigger: "manual",
            messages: 6,
            summary: "Preserved the active task, relevant files, and UI decisions while trimming earlier exploratory context.",
          },
        });
        emit({ kind: "message", text: "Process card preview complete." });
        emitMockTurnDone();
        return;
      }
      // Simulate the server's pre-first-token latency so the deferred user bubble
      // and the "un-send on Esc before any reply" path are observable in browser
      // dev. Bail if cancelled during the wait — nothing was streamed yet.
      await delay(700);
      if (cancelled) return;
      const reply =
        `You said: **${input}**\n\n` +
        "This is the browser dev mock — the real reply comes from the kernel " +
        "inside the Wails shell. Here's a fenced block to exercise the editor seam:\n\n" +
        "```go\nfunc main() {\n    println(\"hello from the mock\")\n}\n```\n";
      for (const ch of reply) {
        if (cancelled) break;
        emit({ kind: "text", text: ch });
        await delay(6);
      }
      emit({ kind: "message", text: reply });
      const toolID = `mock-edit-${Date.now()}`;
      emit({
        kind: "tool_dispatch",
        tool: {
          id: toolID,
          name: "edit_file",
          args: '{"path":"main.go","old_string":"println(\\"hi\\")","new_string":"println(\\"hello\\")"}',
          readOnly: false,
        },
      });
      await delay(350);
      emit({
        kind: "tool_result",
        tool: { id: toolID, name: "edit_file", output: "edited main.go", readOnly: false, durationMs: 350 },
      });
      emit({
        kind: "usage",
        usage: {
          promptTokens: 1280,
          completionTokens: 64,
          totalTokens: 1344,
          cacheHitTokens: 1024,
          cacheMissTokens: 256,
          sessionCacheHitTokens: 1024,
          sessionCacheMissTokens: 256,
        },
      });
          emitMockTurnDone();
        },
        async SubmitToTab(_tabID, input) {
          await withMockTabScope(_tabID, () => this.Submit(input));
        },
        async SubmitDisplay(_display, input) {
          await this.Submit(input);
        },
        async SubmitDisplayToTab(_tabID, display, input) {
          await withMockTabScope(_tabID, () => this.SubmitDisplay(display, input));
        },
        async RunShell(command) {
          cancelled = false;
          emitMockTurnStarted();
          await delay(100);
          if (cancelled) return;
          const id = `shell-${command.slice(0, 32)}`;
          emit({ kind: "tool_dispatch", tool: { id, name: "bash", args: JSON.stringify({ command }), readOnly: false } });
          await delay(200);
          if (cancelled) return;
          emit({ kind: "tool_progress", tool: { id, name: "bash", output: `$ ${command}\n(mock output)\n`, readOnly: false } });
          await delay(100);
          if (cancelled) return;
          emit({ kind: "tool_result", tool: { id, name: "bash", output: `$ ${command}\n(mock output)\n`, readOnly: false, durationMs: 300 } });
          emitMockTurnDone();
        },
        async RunShellForTab(_tabID, command) {
          await withMockTabScope(_tabID, () => this.RunShell(command));
        },
        async Steer(_text) {
          // Mock: emit a steer event as confirmation in the transcript.
          emit({ kind: "steer", text: _text });
        },
        async SteerForTab(_tabID, _text) {
          await this.Steer(_text);
        },
        async Cancel() {
          cancelled = true;
          emitMockTurnDone();
        },
        async CancelTab(_tabID) {
          await withMockTabScope(_tabID, () => this.Cancel());
        },
        async RequestCancelTab(_tabID) {
          await withMockTabScope(_tabID, () => this.Cancel());
          return { accepted: true, turnId: "mock-turn" };
        },
        async Approve(_id, allow, session, persist) {
          if (!pendingApprovalPreview) return;
          pendingApprovalPreview = false;
          const suffix = persist ? "grant saved" : session ? "grant active this session" : "allowed once";
          emit({
            kind: "message",
            text: `approval preview answered: ${allow ? suffix : "denied"}`,
          });
          emitMockTurnDone();
        },
        async ApproveTab(_tabID, id, allow, session, persist) {
          await withMockTabScope(_tabID, () => this.Approve(id, allow, session, persist));
        },
        async AnswerQuestion(_id, answers) {
      if (!pendingAskPreview) return;
      pendingAskPreview = false;
      const summary = answers
        .map((answer) => `${answer.questionId}: ${(answer.selected ?? []).join(", ") || "(no answer)"}`)
        .join("\n");
      emit({ kind: "message", text: `ask preview answered:\n\n${summary}` });
          emitMockTurnDone();
        },
        async AnswerQuestionForTab(_tabID, id, answers) {
          await withMockTabScope(_tabID, () => this.AnswerQuestion(id, answers));
        },
        async ReplayPendingPrompts() {},
        async ConfirmAction(req) {
          void req;
          return false;
        },
        async SetPlanMode(on) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetModeForTab(active.id, modeWithPlan(normalizeMode(active.mode), on));
        },
        async SetMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetModeForTab(active.id, mode);
        },
        async SetModeForTab(tabID, mode) {
          const nextMode = normalizeMode(mode);
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID
              ? {
                  ...tab,
                  mode: nextMode,
                  collaborationMode: normalizeCollaborationMode(undefined, tab.goal, nextMode),
                  toolApprovalMode: normalizeToolApprovalMode(undefined, nextMode),
                }
              : tab,
          );
        },
        async SetCollaborationMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetCollaborationModeForTab(active.id, mode);
        },
        async SetCollaborationModeForTab(tabID, mode) {
          const next = normalizeCollaborationMode(mode);
          mockTabs = mockTabs.map((tab) => {
            if (tab.id !== tabID) return tab;
            const toolMode = normalizeToolApprovalMode(tab.toolApprovalMode, normalizeMode(tab.mode));
            return {
              ...tab,
              collaborationMode: next,
              goal: next === "normal" || next === "plan" ? "" : tab.goal,
              mode: modeWithPlan(modeWithAutoApproveTools(normalizeMode(tab.mode), toolMode === "yolo"), next === "plan"),
            };
          });
        },
        async SetToolApprovalMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetToolApprovalModeForTab(active.id, mode);
        },
        async SetToolApprovalModeForTab(tabID, mode) {
          const next = normalizeToolApprovalMode(mode);
          settings.autoApproveTools = next === "yolo";
          settings.bypass = next === "yolo";
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID
              ? {
                  ...tab,
                  toolApprovalMode: next,
                  mode: modeWithAutoApproveTools(normalizeMode(tab.mode), next === "yolo"),
                }
              : tab,
          );
        },
        async SetAskWorkflowForTab(tabID, enabled) {
          mockTabs = mockTabs.map((tab) => (tab.id === tabID ? { ...tab, askWorkflowEnabled: enabled } : tab));
        },
        async SetStepThinkingForTab(tabID, enabled) {
          mockTabs = mockTabs.map((tab) => (tab.id === tabID ? { ...tab, stepThinkingEnabled: enabled } : tab));
        },
        async SetPromptModeForTab(tabID, mode) {
		  const next = mode === "coding" ? "coding" : "assistant";
		  mockTabs = mockTabs.map((tab) => (tab.id === tabID ? { ...tab, promptMode: next, enhancedModeEnabled: false } : tab));
        },
		async SetConversationModeForTab(tabID, mode) {
		  await this.SetPromptModeForTab(tabID, mode);
		},
		async RequestConversationModeForTab(tabID, mode) {
		  await this.SetPromptModeForTab(tabID, mode);
		  return { requestedMode: mode as PromptMode, appliedMode: mode as PromptMode, generation: 1, completed: true };
		},
        async SetEnhancedModeForTab(tabID, _enabled) {
		  await this.SetPromptModeForTab(tabID, "coding");
        },
        async PauseTab(tabID) {
          mockTabs = mockTabs.map((tab) => (tab.id === tabID ? { ...tab, paused: true } : tab));
        },
        async ResumeTab(tabID) {
          mockTabs = mockTabs.map((tab) => (tab.id === tabID ? { ...tab, paused: false } : tab));
        },
        async SetGoal(goal) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetGoalForTab(active.id, goal);
        },
        async SetGoalForTab(tabID, goal) {
          const nextGoal = goal.trim();
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID
              ? {
                  ...tab,
                  goal: nextGoal,
                  goalStatus: nextGoal ? "running" : "stopped",
                  collaborationMode: nextGoal ? "goal" : "normal",
                  mode: modeWithPlan(normalizeMode(tab.mode), false),
                }
              : tab,
          );
        },
        async ClearGoal() {
          await this.SetGoal("");
        },
        async ClearGoalForTab(tabID) {
          await this.SetGoalForTab(tabID, "");
        },
        async Compact() {},
        async NewSession() {},
        async ClearSession() {},
    async Checkpoints() {
      return [
        { turn: 0, prompt: "你好呀", files: ["src/App.tsx"], time: Date.now() - 30_000, canCode: true, canConversation: true },
      ];
    },
    async CheckpointsForTab() {
      return this.Checkpoints();
    },
    async Rewind() {},
    async Fork() {
      const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
      const tab: TabMeta = {
        ...active,
        id: "tab_fork_" + Date.now(),
        topicId: "topic_fork_" + Date.now(),
        topicTitle: `${active.topicTitle || t("rewind.fork")} · fork`,
        active: true,
        running: false,
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async SummarizeFrom() {},
    async SummarizeUpTo() {},
        async History() {
          return [];
        },
        async HistoryForTab(tabID?: string) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active);
          if (tab?.topicId) {
            queueMockTopicRuntime(tab);
            return mockTopicHistory(tab.topicId);
          }
          return this.History();
        },
    async ListSessions() {
      return sessions.map((s) => ({ ...s }));
    },
    async ListTrashedSessions() {
      return trashedSessions.map((s) => ({ ...s }));
    },
    async ResumeSession(path: string) {
      sessions.forEach((s) => {
        s.current = s.path === path;
        s.open = s.open || s.path === path;
      });
      return [
        { role: "user", content: `(mock) resumed ${path}` },
        { role: "assistant", content: "This is a mock resumed transcript — the real one comes from the kernel." },
      ];
    },
    async ResumeSessionForTab(_tabID: string, path: string) {
      return this.ResumeSession(path);
    },
    async PreviewSession(path: string) {
      const s = sessions.find((x) => x.path === path) ?? trashedSessions.find((x) => x.path === path);
      return [
        { role: "user", content: s?.preview || `(mock) preview ${path}` },
        { role: "phase", content: "Preparing read-only preview" },
        {
          role: "assistant",
          content: "This is a read-only mock preview. The active conversation is unchanged.",
          reasoning: "Preview reads the saved session without resuming it.",
        },
        { role: "notice", level: "info", content: "Preview mode keeps the active conversation untouched." },
        { role: "compaction", content: "", trigger: "manual", messages: 3, summary: "Mock preview preserved the latest task, tool result, and answer summary." },
      ];
    },
    async DeleteSession(path: string) {
      const i = sessions.findIndex((s) => s.path === path);
      if (i >= 0) {
        const [s] = sessions.splice(i, 1);
        trashedSessions.unshift({
          ...s,
          current: false,
          open: false,
          path: s.path.replace("/mock/sessions/", "/mock/sessions/.trash/"),
          deletedAt: Date.now(),
        });
      }
    },
    async RestoreSession(path: string) {
      const i = trashedSessions.findIndex((s) => s.path === path);
      if (i >= 0) {
        const [s] = trashedSessions.splice(i, 1);
        sessions.unshift({
          ...s,
          path: s.path.replace("/mock/sessions/.trash/", "/mock/sessions/"),
          deletedAt: undefined,
        });
      }
    },
    async PurgeTrashedSession(path: string) {
      const i = trashedSessions.findIndex((s) => s.path === path);
      if (i >= 0) trashedSessions.splice(i, 1);
    },
    async RenameSession(path: string, title: string) {
      const s = sessions.find((x) => x.path === path);
      if (s) s.title = title.trim() || undefined;
    },
    async ListWorkspaces() {
      return mockProjectTree
        .filter((node) => node.kind === "project" && node.root)
        .map((node) => ({
          path: node.root!,
          name: node.label || baseName(node.root!),
          current: node.root === cwd,
        }));
    },
    async PickWorkspace() {
      // Browser dev has no native dialog; simulate picking a folder and re-root so
      // the topbar folder chip visibly changes.
      return mockSwitchWorkspace(cwd.endsWith("another-project") ? "~/projects/orca" : "~/projects/another-project");
    },
    async SwitchWorkspace(path: string) {
      return mockSwitchWorkspace(path);
    },
    async RemoveWorkspace(path: string) {
      workspaces = workspaces.filter((p) => p !== path);
      const index = mockProjectTree.findIndex((node) => node.root === path);
      if (index >= 0) mockProjectTree.splice(index, 1);
    },
        async ContextUsage() {
          return { used: 42124, window: 128000, sessionTokens: 34479, compactRatio: 0.8 };
        },
        async ContextUsageForTab() {
          return this.ContextUsage();
        },
        async Balance() {
      // Mirror the active mock provider: deepseek-flash carries a balance_url.
      const p = settings.providers.find((x) => x.name === settings.defaultModel);
      if (!p?.balanceUrl) return { available: false, display: "" };
          return { available: true, display: "¥128.50" };
        },
        async BalanceForTab() {
          return this.Balance();
        },
        async Jobs() {
          return []; // browser dev mock has no background jobs
        },
        async JobsForTab() {
          return this.Jobs();
        },
        async Meta() {
          const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
          const toolApprovalMode = normalizeToolApprovalMode(active?.toolApprovalMode, active ? normalizeMode(active.mode) : "normal", settings.autoApproveTools);
          const autoApproveTools = toolApprovalMode === "yolo";
          return {
            label: active?.label ?? "DeepSeek-R1",
            ready: active?.ready ?? true,
            eventChannel: EVENT_CHANNEL,
            cwd: active?.cwd || cwd,
            autoApproveTools,
            bypass: autoApproveTools,
            toolApprovalMode,
            askWorkflowEnabled: active?.askWorkflowEnabled ?? false,
            stepThinkingEnabled: active?.stepThinkingEnabled ?? false,
			promptMode: active?.promptMode ?? "assistant",
            enhancedModeEnabled: active?.enhancedModeEnabled ?? false,
            paused: active?.paused ?? false,
            goal: active?.goal ?? "",
            goalStatus: active?.goalStatus ?? (active?.goal ? "running" : "stopped"),
            readOnly: active?.readOnly ?? false,
            automationFullAccessApproved: settings.automationFullAccessApproved,
          };
        },
        async MetaForTab(tabID) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active) ?? mockTabs[0];
          const toolApprovalMode = normalizeToolApprovalMode(tab?.toolApprovalMode, tab ? normalizeMode(tab.mode) : "normal", settings.autoApproveTools);
          const autoApproveTools = toolApprovalMode === "yolo";
          return {
            label: tab?.label ?? "DeepSeek-R1",
            ready: tab?.ready ?? true,
            eventChannel: EVENT_CHANNEL,
            cwd: tab?.cwd || cwd,
            autoApproveTools,
            bypass: autoApproveTools,
            toolApprovalMode,
            askWorkflowEnabled: tab?.askWorkflowEnabled ?? false,
            stepThinkingEnabled: tab?.stepThinkingEnabled ?? false,
			promptMode: tab?.promptMode ?? "assistant",
            enhancedModeEnabled: tab?.enhancedModeEnabled ?? false,
            paused: tab?.paused ?? false,
            goal: tab?.goal ?? "",
            goalStatus: tab?.goalStatus ?? (tab?.goal ? "running" : "stopped"),
            readOnly: tab?.readOnly ?? false,
            automationFullAccessApproved: settings.automationFullAccessApproved,
          };
        },
    async Commands() {
      return [
        { name: "new", description: "开启新会话并保存当前记录", kind: "builtin" as const },
        { name: "clear", description: "清空当前上下文", kind: "builtin" as const },
        { name: "compact", description: "压缩较早历史以释放上下文", kind: "builtin" as const },
        { name: "model", description: "切换模型", kind: "builtin" as const },
        { name: "effort", description: "设置思考强度", kind: "builtin" as const },
        { name: "skill", description: "查看 Skill", kind: "builtin" as const },
        { name: "explore", description: "用隔离 subagent 调研代码库", kind: "skill" as const },
        { name: "review", description: "审查暂存区 diff", hint: "[关注点]", kind: "custom" as const },
      ];
    },
    async Capabilities() {
      return {
        servers: capServers.map((s) => ({ ...s })),
        skills: capSkills.map((s) => ({ ...s })),
        skillRoots: capSkillRoots.map((s) => ({ ...s })),
      };
    },
    async AddMCPServer(input: MCPServerInput) {
      const tools = input.transport === "stdio" ? 3 : 5;
      capServers.push({
        name: input.name,
        transport: input.transport,
        status: "connected",
        configured: true,
        autoStart: true,
        tier: "background",
        command: input.command,
        args: input.args,
        url: input.url,
        tools,
        prompts: 0,
        resources: 0,
        toolList: Array.from({ length: tools }, (_, i) => ({
          name: `${input.name}_tool_${i + 1}`,
          description: `${input.name} 暴露的模拟工具 ${i + 1}。`,
        })),
      });
      return tools;
    },
    async UpdateMCPServer(name: string, input: MCPServerInput) {
      capServers = capServers.map((s) => {
        if (s.name !== name) return s;
        const connected = s.status === "connected" || s.status === "failed" || s.tier !== "lazy";
        const nextStatus = s.status === "disabled" ? "disabled" : connected ? "connected" : "deferred";
        const nextTools = nextStatus === "connected" ? s.tools || (input.transport === "stdio" ? 3 : 5) : 0;
        return {
          ...s,
          transport: input.transport,
          status: nextStatus,
          command: input.transport === "stdio" ? input.command : "",
          args: input.transport === "stdio" ? input.args : [],
          url: input.transport === "stdio" ? "" : input.url,
          envKeys: input.env ? Object.keys(input.env).sort() : s.envKeys,
          tools: nextTools,
          error: undefined,
          authStatus: nextStatus !== "connected" && input.transport !== "stdio" ? "possible" : undefined,
          authUrl: nextStatus !== "connected" && input.transport !== "stdio" ? input.url : undefined,
        };
      });
    },
    async RemoveMCPServer(name: string) {
      capServers = capServers.filter((s) => s.name !== name);
    },
    async ReconnectMCPServer(name: string) {
      capServers = capServers.map((s) =>
        s.name === name
          ? { ...s, status: "initializing", error: undefined, authStatus: undefined, authUrl: undefined }
          : s,
      );
      await new Promise((r) => setTimeout(r, 400));
      capServers = capServers.map((s) =>
        s.name === name ? { ...s, status: "connected", tools: s.tools || 4 } : s,
      );
    },
    async ClearMCPServerAuthentication(name: string) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: s.tier === "background" || s.tier === "eager" ? "initializing" : "deferred",
              tools: 0,
              error: undefined,
              authStatus: s.transport !== "stdio" ? "possible" : undefined,
              authUrl: s.transport !== "stdio" ? s.url : undefined,
              authConfigured: undefined,
            }
          : s,
      );
    },
    async PickSkillFolder() {
      return "~/my-skills";
    },
    async AddSkillPath(path: string) {
      const dir = path.trim() || "~/my-skills";
      if (!capSkillRoots.some((r) => r.scope === "custom" && r.dir === dir)) {
        capSkillRoots.push({
          dir,
          scope: "custom",
          priority: capSkillRoots.length + 1,
          status: "ok",
          configured: true,
          removable: true,
          skills: 1,
          skillItems: [{ name: "local-dev", description: "本地自定义开发流程", scope: "custom", runAs: "inline" }],
        });
      }
      if (!capSkills.some((s) => s.name === "local-dev")) {
        capSkills.push({ name: "local-dev", description: "本地自定义开发流程", scope: "custom", runAs: "inline", enabled: true });
      }
    },
    async RemoveSkillPath(path: string) {
      capSkillRoots = capSkillRoots.filter((r) => r.dir !== path);
      if (!capSkillRoots.some((r) => r.scope === "custom")) {
        const idx = capSkills.findIndex((s) => s.name === "local-dev");
        if (idx >= 0) capSkills.splice(idx, 1);
      }
    },
    async RefreshSkills() {},
    async SetSkillEnabled(name: string, enabled: boolean) {
      const skill = capSkills.find((s) => s.name === name);
      if (skill) skill.enabled = enabled;
    },
    async SetMCPServerEnabled(name: string, enabled: boolean) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: enabled ? "connected" : "disabled",
              autoStart: s.builtIn ? enabled : s.autoStart,
              tools: enabled ? s.tools || 4 : 0,
              error: undefined,
              authStatus: !enabled && s.transport !== "stdio" ? "possible" : undefined,
              authUrl: !enabled && s.transport !== "stdio" ? s.url : undefined,
            }
          : s,
      );
    },
    async SetMCPServerTier(name: string, tier: string) {
      capServers = capServers.map((s) => {
        if (s.name !== name) return s;
        if (tier === "lazy") return { ...s, tier, autoStart: true };
        const tools = s.tools || (s.transport === "stdio" ? 3 : 5);
        return { ...s, tier, autoStart: true, status: "connected", tools, error: undefined, authStatus: undefined, authUrl: undefined };
      });
    },
    async SlashArgs(input: string) {
      // Mirror a slice of the real arg hints so the menu is exercisable in browser dev.
      const from = input.lastIndexOf(" ") + 1;
      const cur = input.slice(from);
      const cmd = input.slice(0, input.indexOf(" ") < 0 ? input.length : input.indexOf(" "));
      const subs: Record<string, { label: string; insert: string; hint: string; descend?: boolean }[]> = {
        "/skill": [
          { label: "list", insert: "list", hint: "列出 Skill" },
          { label: "show", insert: "show ", hint: "查看 Skill 内容", descend: true },
          { label: "enable", insert: "enable ", hint: "启用已停用的 Skill", descend: true },
          { label: "disable", insert: "disable ", hint: "停用已启用的 Skill", descend: true },
          { label: "new", insert: "new ", hint: "创建新的 Skill" },
          { label: "paths", insert: "paths", hint: "查看发现路径" },
        ],
        "/hooks": [
          { label: "list", insert: "list", hint: "列出已启用 hook" },
          { label: "trust", insert: "trust", hint: "信任此项目的 hook" },
        ],
        "/model": [
          { label: "deepseek/deepseek-v4-flash", insert: "deepseek/deepseek-v4-flash", hint: "当前" },
          { label: "deepseek/deepseek-v4-pro", insert: "deepseek/deepseek-v4-pro", hint: "" },
        ],
        "/effort": [
          { label: "auto", insert: "auto", hint: "使用模型默认值" },
          { label: "high", insert: "high", hint: "更深入思考" },
          { label: "max", insert: "max", hint: "最高思考强度" },
        ],
      };
      const items = (subs[cmd] ?? [])
        .filter((it) => it.label.toLowerCase().startsWith(cur.toLowerCase()))
        .map((it) => ({ label: it.label, insert: it.insert, hint: it.hint, descend: it.descend ?? false }));
      return { items, from };
    },
    async ListDir(rel: string) {
      // A tiny fake tree so the @ menu is navigable in browser dev.
      if (rel === "" || rel === "./") {
        return [
          { name: "internal", isDir: true },
          { name: "desktop", isDir: true },
          { name: "README.md", isDir: false },
          { name: "go.mod", isDir: false },
        ];
      }
      if (rel === "internal/") {
        return [
          { name: "control", isDir: true },
          { name: "boot", isDir: true },
          { name: "event.go", isDir: false },
        ];
      }
      return [{ name: "file.go", isDir: false }];
    },
    async SearchFileRefs(query: string) {
      const q = query.toLowerCase();
      return ["desktop/frontend/src/lib/bridge.ts", "frontend/wailsjs/runtime/runtime.js", "internal/control/refs.go"]
        .filter((path) => path.split("/").pop()?.toLowerCase().includes(q))
        .map((name) => ({ name, isDir: false }));
    },
    async ReadFile(rel: string) {
      const samples: Record<string, string> = {
        "README.md": "# O.R.C.A.\n\nBrowser-dev workspace preview.\n\n- Chat in the center\n- Browse files on the right\n- Keep sessions on the left\n",
        "go.mod": "module orca-agent\n\ngo 1.23\n",
        "desktop/file.go": "package desktop\n\nfunc main() {\n\tprintln(\"workspace preview\")\n}\n",
        "internal/event.go": "package internal\n\n// mock file used by the browser dev seam\n",
      };
      return {
        path: rel,
        body: samples[rel] ?? `// ${rel}\n\nMock file body from browser dev.`,
        size: samples[rel]?.length ?? 42,
        truncated: false,
        binary: false,
      };
    },
    async WorkspaceChanges() {
      return {
        gitAvailable: true,
        gitBranch: "main",
        files: [
          {
            path: "desktop/frontend/src/components/WorkspacePanel.tsx",
            sources: ["session", "git"],
            gitStatus: "M",
            turns: [0, 2],
            latestPrompt: "Mock session edited the workspace panel.",
            latestTime: Date.now() - 60_000,
          },
          { path: "README.md", sources: ["git"], gitStatus: "??" },
          { path: "internal/control/controller.go", sources: ["session"], turns: [1], latestTime: Date.now() - 120_000 },
        ],
      };
    },
    async GitBranches() {
      return ["main", "dev", "feature/branch-switcher"];
    },
    async GitCheckout(_branch: string) {
      console.info("mock GitCheckout", _branch);
    },
    async WorkspaceGitHistory(path: string) {
      return [
        { hash: "abcdef123456", author: "Mock Author", date: new Date().toISOString(), message: "Mock commit message for " + path },
      ];
    },
    async WorkspaceGitCommitDetail(_hash: string, path: string) {
      if (path) {
        return { diff: "--- a/mock\n+++ b/mock\n@@ -1,1 +1,1 @@\n-mock\n+mock diff" };
      }
      return { files: ["mock_file_1.ts", "mock_file_2.ts"] };
    },
    async OpenWorkspacePath(rel: string) {
      console.info("mock OpenWorkspacePath", rel);
    },
    async RevealWorkspacePath(rel: string) {
      console.info("mock RevealWorkspacePath", rel);
    },
    async RevealPath(path: string) {
      console.info("mock RevealPath", path);
    },
    async SavePastedImage(_dataUrl: string) {
      return ".orca/attachments/mock.png";
    },
    async SaveClipboardImage() {
      return ".orca/attachments/mock-clipboard.png";
    },
    async ReadClipboardFilePaths() {
      return [];
    },
    async SavePastedFile(name: string, _dataUrl: string) {
      return `.orca/attachments/mock-${name}`;
    },
    async PickExportFile(defaultFilename: string, _mimeType: string) {
      return defaultFilename;
    },
    async SaveExportFile(path: string, payload: string, base64Encoded: boolean) {
      const a = document.createElement("a");
      let url = "";
      if (base64Encoded) {
        url = `data:application/octet-stream;base64,${payload}`;
      } else {
        url = URL.createObjectURL(new Blob([payload], { type: "text/plain;charset=utf-8" }));
      }
      a.href = url;
      a.download = path;
      document.body.appendChild(a);
      a.click();
      a.remove();
      if (!base64Encoded) URL.revokeObjectURL(url);
    },
    async AttachDropped(path: string) {
      const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
      return { kind: "attachment" as const, path: `.orca/attachments/mock-${name}` };
    },
    async AttachmentDataURL(_path: string) {
      return "data:image/png;base64,iVBORw0KGgo=";
    },
        async Models() {
          const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
          const current = mockTabModelRef(active);
          return mockModelCatalog.map((model) => ({ ...model, current: model.ref === current }));
        },
        async ModelsForTab(tabID) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active) ?? mockTabs[0];
          const current = mockTabModelRef(tab);
          return mockModelCatalog.map((model) => ({ ...model, current: model.ref === current }));
        },
        async SetModel(name) {
          setMockTabModel(undefined, name);
        },
        async SetModelForTab(tabID, name) {
          setMockTabModel(tabID, name);
        },
        async Effort() {
          return { supported: true, current: mockEffort, default: "high", levels: ["auto", "high", "max"] };
        },
        async EffortForTab() {
          return this.Effort();
        },
        async SetEffort(level: string) {
          mockEffort = level || "auto";
        },
        async SetEffortForTab(_tabID, level) {
          await this.SetEffort(level);
        },
    async Memory() {
      return {
        available: true,
        storeDir: "~/.config/orca/projects/-mock/memory",
        docs: [
          {
            path: "ORCA.md",
            scope: "project",
            body: "# O.R.C.A. project memory\n\nMock doc shown in the browser dev seam.\n\n## Notes\n\n- prefers concise replies",
          },
          {
            path: "~/.config/orca/ORCA.md",
            scope: "user",
            body: t("mock.memoryBody"),
          },
        ],
        facts: [
          {
            name: "prefers-tabs",
            description: "User prefers tabs",
            type: "user",
            body: "Indent with tabs.",
            profile: "shared-agent",
          },
          {
            name: "prefers-warm-chat",
            description: "User likes a warmer assistant style in assistant mode",
            type: "feedback",
            body: "Use a warm, natural tone when it fits.",
            profile: "assistant",
            source: "auto",
            confidence: 0.86,
          },
        ],
        scopes: [
          { scope: "user", path: "~/.config/orca/ORCA.md" },
          { scope: "project", path: "ORCA.md" },
          { scope: "local", path: "ORCA.local.md" },
        ],
      };
    },
    async Remember(scope: string, note: string) {
      emit({ kind: "notice", level: "info", text: `remembered → ${scope}` });
      return `${scope} ORCA.md (mock): ${note}`;
    },
    async Forget(name: string) {
      emit({ kind: "notice", level: "info", text: `forgot → ${name}` });
    },
    async SaveDoc(path: string, _body: string) {
      emit({ kind: "notice", level: "info", text: `saved → ${path}` });
      return path;
    },
    async GetAssistantMemorySettings() {
      return { ...mockAssistantMemorySettings };
    },
    async SetAssistantMemorySettings(settings: AssistantMemorySettings) {
      mockAssistantMemorySettings = { ...settings };
    },
    async ClearAssistantMemories() {
      emit({ kind: "notice", level: "info", text: "cleared assistant memories" });
    },
    async Settings() {
      return JSON.parse(JSON.stringify(settings)) as SettingsView;
    },
    async GetWelcomeSuggestions() {
      return [];
    },
        async SetDefaultModel(ref: string) {
          settings.defaultModel = ref;
        },
        async SetAutomationModel(ref: string) {
          settings.automationModel = ref;
          settings.bot.model = ref;
        },
        async SetAutomationFullAccess(enabled: boolean) {
          settings.automationFullAccessApproved = enabled;
        },
    async SetPlannerModel(ref: string) {
      settings.plannerModel = ref;
    },
    async SetSubagentModel(ref: string) {
      settings.subagentModel = ref;
    },
    async SetSubagentEffort(level: string) {
      settings.subagentEffort = level;
    },
    async SetVisionModel(ref: string) {
      settings.visionModel = ref;
    },
    async SetAutoPlan(mode: string) {
      settings.autoPlan = mode;
    },
    async SaveProvider(p: ProviderView) {
      p.added = true;
      const i = settings.providers.findIndex((x) => x.name === p.name);
      if (i >= 0) settings.providers[i] = p;
      else settings.providers.push(p);
    },
    async AddOfficialProviderAccess(kind: string, key: string) {
      const templates: Record<string, ProviderView> = {
        deepseek: { name: "deepseek", builtIn: true, added: true, kind: "openai", baseUrl: "https://api.deepseek.com", modelsUrl: "", models: ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"], default: "deepseek-v4-flash", apiKeyEnv: "DEEPSEEK_API_KEY", keySet: !!key.trim(), balanceUrl: "https://api.deepseek.com/user/balance", contextWindow: 1_000_000, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
        "mimo-api": { name: "mimo-api", builtIn: true, added: true, kind: "openai", baseUrl: "https://api.xiaomimimo.com/v1", modelsUrl: "", models: ["mimo-v2.5", "mimo-v2.5-pro"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_API_KEY", keySet: !!key.trim(), balanceUrl: "", contextWindow: 1_048_576, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
        "mimo-token-plan": { name: "mimo-token-plan", builtIn: true, added: true, kind: "openai", baseUrl: "https://token-plan-cn.xiaomimimo.com/v1", modelsUrl: "", models: ["mimo-v2.5-pro"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_API_KEY", keySet: !!key.trim(), balanceUrl: "", contextWindow: 1_048_576, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
      };
      const next = templates[kind] ?? templates.deepseek;
      const i = settings.providers.findIndex((x) => x.name === next.name);
      if (i >= 0) settings.providers[i] = { ...settings.providers[i], ...next, keySet: next.keySet || settings.providers[i].keySet };
      else settings.providers.push(next);
    },
    async FetchProviderModels(p: ProviderView) {
      if (!p.baseUrl.trim()) throw new Error(t("settings.fetchModelsMissingBaseUrl"));
      if (!p.apiKeyEnv.trim()) throw new Error(t("settings.fetchModelsMissingKeyEnv"));
      await delay(350);
      if (p.baseUrl.includes("deepseek")) return ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"];
      if (p.baseUrl.includes("mimo") || p.baseUrl.includes("xiaomimimo")) return ["mimo-v2.5", "mimo-v2.5-pro"];
      return ["gpt-5", "gpt-5-mini", "qwen3-coder"];
    },
    async DeleteProvider(name: string) {
      settings.providers = settings.providers.filter((p) => p.name !== name);
    },
    async RemoveProviderAccess(name: string) {
      const p = settings.providers.find((x) => x.name === name);
      if (p?.builtIn) p.added = false;
      else settings.providers = settings.providers.filter((x) => x.name !== name);
    },
    async SetProviderKey(apiKeyEnv: string, _value: string) {
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === apiKeyEnv) p.keySet = true;
      });
    },
    async ClearProviderKey(apiKeyEnv: string) {
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === apiKeyEnv) p.keySet = false;
      });
    },
    async SetPermissionMode(mode: string) {
      settings.permissions.mode = mode;
    },
    async SetAutoReviewModel(ref: string) {
      settings.permissions.autoReviewModel = ref;
    },
    async AddPermissionRule(list: string, rule: string) {
      const k = list as "allow" | "ask" | "deny";
      if (settings.permissions[k] && !settings.permissions[k].includes(rule)) settings.permissions[k].push(rule);
    },
    async RemovePermissionRule(list: string, rule: string) {
      const k = list as "allow" | "ask" | "deny";
      settings.permissions[k] = settings.permissions[k].filter((r) => r !== rule);
    },
        async SetSandbox(bash: string, network: boolean, workspaceRoot: string, allowWrite: string[]) {
          settings.sandbox = { bash, network, workspaceRoot, allowWrite };
        },
        async SetNetwork(n: NetworkView) {
          settings.network = n;
        },
        async SetBotSettings(b: BotSettingsView) {
          settings.bot = JSON.parse(JSON.stringify(b)) as BotSettingsView;
        },
        async SetBotSecret(envName: string, _value: string) {
          const name = envName.trim();
          if (settings.bot.qq.appSecretEnv === name) settings.bot.qq.secretSet = true;
          if (settings.bot.feishu.appSecretEnv === name) settings.bot.feishu.secretSet = true;
          if (settings.bot.weixin.tokenEnv === name) settings.bot.weixin.tokenSet = true;
        },
        async ClearBotSecret(envName: string) {
          const name = envName.trim();
          if (settings.bot.qq.appSecretEnv === name) settings.bot.qq.secretSet = false;
          if (settings.bot.feishu.appSecretEnv === name) settings.bot.feishu.secretSet = false;
          if (settings.bot.weixin.tokenEnv === name) settings.bot.weixin.tokenSet = false;
        },
        async ConnectQQBot(req: { appId: string; appSecret: string; environment: string }) {
          const now = new Date().toISOString();
          const environment = req.environment === "production" ? "production" : "sandbox";
          settings.bot.enabled = true;
          settings.bot.qq = {
            ...settings.bot.qq,
            enabled: true,
            appId: req.appId.trim(),
            appSecretEnv: "QQ_BOT_APP_SECRET",
            secretSet: true,
            environment,
          };
          const connection = {
            id: "qq-qq",
            provider: "qq",
            domain: "qq",
            label: "QQ",
            enabled: true,
            status: "connected",
            credential: {
              appId: req.appId.trim(),
              appSecretEnv: "QQ_BOT_APP_SECRET",
              accountId: "",
              tokenEnv: "",
              environment,
              secretSet: true,
            },
            sessionMappings: [],
            guideSent: false,
            lastError: "",
            createdAt: now,
            updatedAt: now,
          };
          settings.bot.connections = [...settings.bot.connections.filter((c) => c.id !== connection.id), connection];
          return connection;
        },
        async StartBotConnectionInstall(provider: string, domain: string) {
          const normalizedProvider = provider === "weixin" ? "weixin" : "feishu";
          const normalizedDomain = normalizedProvider === "weixin" ? "weixin" : domain === "lark" ? "lark" : "feishu";
          return {
            ok: true,
            provider: normalizedProvider,
            domain: normalizedDomain,
            installId: `mock-${normalizedProvider}-${normalizedDomain}`,
            url: "https://example.com/orca-bot-qr",
            deviceCode: "MOCKDEVICE",
            userCode: normalizedProvider === "weixin" ? "" : "MOCK-CODE",
            interval: 3,
            expireIn: 300,
            message: "",
          };
        },
        async PollBotConnectionInstall(installID: string) {
          const isWeixin = installID.includes("weixin");
          const domain = installID.includes("lark") ? "lark" : isWeixin ? "weixin" : "feishu";
          const provider = isWeixin ? "weixin" : "feishu";
          const connection = {
            id: `${provider}-${domain}`,
            provider,
            domain,
            label: domain === "lark" ? "Lark" : domain === "weixin" ? "微信" : "飞书",
            enabled: true,
            status: "connected",
            credential: {
              appId: provider === "feishu" ? "cli_mock" : "",
              appSecretEnv: provider === "feishu" ? (domain === "lark" ? "LARK_BOT_APP_SECRET" : "FEISHU_BOT_APP_SECRET") : "",
              accountId: provider === "weixin" ? "mock-account" : "",
              tokenEnv: provider === "weixin" ? "WEIXIN_BOT_TOKEN" : "",
              environment: "",
              secretSet: true,
            },
            sessionMappings: [],
            guideSent: false,
            lastError: "",
            createdAt: new Date().toISOString(),
            updatedAt: new Date().toISOString(),
          };
          settings.bot.connections = [...settings.bot.connections.filter((c) => c.id !== connection.id), connection];
          return { done: true, connection, status: "connected", message: "connected", error: "" };
        },
        async DiagnoseBotConnection(id: string) {
          const connection = settings.bot.connections.find((c) => c.id === id);
          return connection
            ? { id, label: connection.label, status: connection.enabled ? "ok" : "disabled", message: connection.enabled ? "连接配置已保存。" : "连接已保存但未启用。", messageId: "" }
            : { id, label: "", status: "missing", message: "未找到连接。", messageId: "" };
        },
        async TestBotConnection(id: string, target?: string) {
          const diag = await this.DiagnoseBotConnection(id);
          if (target?.trim()) return { ...diag, message: `Mock test sent to ${target.trim()}`, messageId: "mock-message-id" };
          return diag;
        },
        async GetBotRuntimeStatus() {
          const channels = settings.bot.connections
            .filter((connection) => connection.enabled && (connection.provider === "qq" || connection.provider === "weixin"))
            .map((connection) => connection.label || connection.provider);
          return {
            status: channels.length > 0 ? "running" : "idle",
            message: channels.length > 0 ? `后台运行中：${channels.join("、")}` : "QQ/微信尚未连接。",
            channels,
          };
        },
        async SetCloseBehavior(mode: string) {
          settings.closeBehavior = mode === "quit" ? "quit" : "background";
        },
        async SetDesktopLanguage(lang: string) {
          settings.desktopLanguage = lang === "en" || lang === "zh" ? lang : "";
        },
        async SetDesktopAppearance(theme: string, style: string) {
          settings.desktopTheme = theme === "auto" || theme === "light" ? theme : "dark";
          settings.desktopThemeStyle = style;
        },
        async SetDesktopUIStyle(style: string) {
          settings.desktopUIStyle = style === "classic" ? "classic" : "modern";
        },
        async RestartApp() {},
    async SetDesktopUIScale(scale: number) {
          void scale;
          settings.uiScale = 0;
          settings.effectiveUIScale = 100;
        },
        async SetDesktopCheckUpdates(enabled: boolean) {
          settings.checkUpdates = enabled;
        },
        async SetExpandThinking(on: boolean) {
          settings.expandThinking = on;
          settings.processDisplayMode = on ? "detailed" : "compact";
        },
        async SetProcessDisplayMode(mode: string) {
          settings.processDisplayMode = mode === "detailed" ? "detailed" : "compact";
          settings.expandThinking = settings.processDisplayMode === "detailed";
        },
        async SetActivityIndicatorEnabled(enabled: boolean) {
          settings.activityIndicatorEnabled = enabled;
        },
        async SetVisionEnabled(enabled: boolean) {
          settings.visionEnabled = enabled;
          settings.visionMode = enabled ? "on" : "off";
        },
        async SetVisionMode(mode: string) {
          settings.visionMode = mode === "off" || mode === "on" ? mode : "auto";
          settings.visionEnabled = settings.visionMode === "on";
        },
        async UpdateProviderModels(name: string, models: string[], defaultModel: string) {
          const provider = settings.providers.find((item) => item.name === name);
          if (!provider) throw new Error(`provider ${name} not found`);
          provider.models = [...models];
          provider.default = defaultModel;
        },
        async GetVisionCapabilities() {
          return mockVisionCapabilities.map((item) => ({ ...item }));
        },
        async ProbeModelVision(modelRef: string) {
          const lower = modelRef.toLowerCase();
          const next: VisionCapability = { modelRef, key: modelRef, status: lower.includes("deepseek-v4-flash-vision-exp") ? "supported" : lower.includes("deepseek") ? "unsupported" : "supported", checkedAt: Date.now() };
          mockVisionCapabilities = [...mockVisionCapabilities.filter((item) => item.modelRef !== modelRef), next];
          return next;
        },
        async SetVisionCapabilityOverride(modelRef: string, override: string) {
          const current = mockVisionCapabilities.find((item) => item.modelRef === modelRef) ?? { modelRef, key: modelRef, status: "unknown" as const };
          const normalized = override === "supported" || override === "unsupported" ? override : "auto";
          const next: VisionCapability = {
            ...current,
            override: normalized,
            status: normalized === "auto" ? current.status : normalized,
            source: normalized === "auto" ? (current.source ?? "probe") : "manual",
          };
          mockVisionCapabilities = [...mockVisionCapabilities.filter((item) => item.modelRef !== modelRef), next];
          return next;
        },
        async MigrateDesktopPreferences(language: string, theme: string, style: string) {
          if (!settings.desktopLanguage) settings.desktopLanguage = language === "en" || language === "zh" ? language : "";
          if (!settings.desktopTheme && !settings.desktopThemeStyle) {
            settings.desktopTheme = theme === "auto" || theme === "light" ? theme : "dark";
            settings.desktopThemeStyle = style;
          }
        },
    async SetAgentParams(temperature: number, maxSteps: number, plannerMaxSteps: number, softCompactRatio: number, compactRatio: number, compactForceRatio: number, systemPrompt: string) {
      settings.agent = { temperature, maxSteps, plannerMaxSteps, softCompactRatio, compactRatio, compactForceRatio, systemPrompt };
    },
    async SetTrayLocale(_locale: "en" | "zh") {},
    async SetAutoApproveTools(on: boolean) {
      await this.SetToolApprovalMode(on ? "yolo" : "ask");
    },
    async SetBypass(on: boolean) {
      await this.SetAutoApproveTools(on);
    },
    async Version() {
      return "v1.0.0 (browser dev)";
    },
    async CheckUpdate() {
      return null;
    },
    async DownloadUpdate() {},
    async SetUpdateSource(_source: string) {},
    async CancelUpdateDownload() {},
    async GetUpdateStatus() {
      return { phase: "idle", received: 0, total: 0 } satisfies UpdateProgress;
    },
    async ApplyUpdate() {},
    async OpenDownloadedUpdate() {},
    async OpenDownloadPage() {
      if (typeof window !== "undefined") {
        window.open("https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/latest", "_blank", "noopener");
      }
    },
    // Dev seam: drives the overlay flow in the browser until ConnectKey sets the
    // key. Matches ConnectKey on apiKeyEnv so the two stay in sync.
    async NeedsOnboarding() {
      return !settings.providers.find((p) => p.apiKeyEnv === "DEEPSEEK_API_KEY")?.keySet;
    },
    async GetOnboardingState() {
      return { required: false, completed: true, hasCloudModel: true, hasLocalRuntime: false, platform: "browser", providers: settings.providers };
    },
    async CompleteOnboarding() { settings.computerUseFullAccessApproved = false; },
    async ConnectProviderPreset(_presetID: string, _apiKey: string) { return settings.providers[0]?.models ?? []; },
    async GetHardwareProfile() {
      return { platform: "browser", supported: false, gpus: [], gpuDetectionFailed: false, memoryTotalMiB: 0, memoryFreeMiB: 0, cpuLogicalCores: 0, diskFreeBytes: 0, recommendedRuntime: "", recommendedModel: "" };
    },
    async GetLocalAICatalog() {
      return { supported: false, platform: "browser", models: [], runtimes: [], installedModels: [], downloads: [], status: { state: "unavailable", supported: false, installed: false }, hardware: await this.GetHardwareProfile(), modelsDirectory: "" };
    },
    async StartLocalRuntimeInstall(_runtimeID: string) { throw new Error("local AI is only available in the Windows app"); },
    async StartLocalModelDownload(_modelID: string) { throw new Error("local AI is only available in the Windows app"); },
    async PauseLocalDownload(_taskID: string) {},
    async ResumeLocalDownload(_taskID: string) {},
    async CancelLocalDownload(_taskID: string) {},
    async StopLocalRuntime() {},
    async UninstallLocalRuntime() {},
    async DeleteLocalModel(_modelID: string) {},
    async SetLocalModelsDirectory(_path: string) {},
    async SetComputerControlModel(modelRef: string) { settings.computerControlModel = modelRef; },
    async GetComputerUseState() {
      return { capabilities: { platform: "browser", supported: false, temporarilyDisabled: true, screenCapture: false, uiAutomation: false, inputInjection: false, overlay: false, emergencyStop: false, unavailableReason: t("settings.computer.temporarilyDisabled") }, session: { id: "", goal: "", state: "idle", actionCount: 0, logs: [] }, approved: settings.computerUseFullAccessApproved, consentVersion: settings.computerUseFullAccessApproved ? 1 : 0, modelRef: settings.computerControlModel };
    },
    async StartComputerUseSession(_request: { tabId?: string; goal: string; successCriteria?: string; restrictions?: string; modelRef?: string }) { throw new Error(t("settings.computer.temporarilyDisabled")); },
    async ObserveComputerUse() { throw new Error(t("settings.computer.temporarilyDisabled")); },
    async ExecuteComputerAction(_action: any) { throw new Error(t("settings.computer.temporarilyDisabled")); },
    async SetComputerUseFullAccess(enabled: boolean) {
      if (enabled) throw new Error(t("settings.computer.temporarilyDisabled"));
      settings.computerUseFullAccessApproved = false;
    },
    async PauseComputerUse() { return (await this.GetComputerUseState()).session; },
    async ResumeComputerUse() { throw new Error(t("settings.computer.temporarilyDisabled")); },
    async StopComputerUse() {},
    async ConnectKey(apiKey: string) {
      if (!apiKey.trim()) throw new Error("key is required");
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === "DEEPSEEK_API_KEY") p.keySet = true;
      });
      await delay(300);
    },
    // Tab management mocks.
    async ListTabs() {
      return mockTabs.map((tab) => ({ ...tab }));
    },
    async OpenProjectTab(workspaceRoot: string, _topicID: string) {
      const existing = mockTabs.find((tab) => tab.scope === "project" && tab.workspaceRoot === workspaceRoot && tab.topicId === _topicID);
      if (existing) {
        const active = { ...existing, active: true, running: mockTopicRunsInScenario(_topicID) };
        mockTabs = mockTabs.map((tab) => (tab.id === existing.id ? active : { ...tab, active: false }));
        return { ...active };
      }
      const tab: TabMeta = {
        id: "tab_" + Date.now(),
        scope: "project",
        workspaceRoot,
        workspaceName: workspaceRoot.split("/").filter(Boolean).pop() ?? workspaceRoot,
        topicId: _topicID,
        topicTitle: topicLabel(_topicID, t("mock.newSession")),
        projectColor: mockProjectTree.find((node) => node.root === workspaceRoot)?.projectColor,
        label: "deepseek-v4-flash",
        ready: true,
        running: mockTopicRunsInScenario(_topicID),
        mode: "normal",
        collaborationMode: "normal",
        toolApprovalMode: "ask",
        active: true,
        cwd: workspaceRoot,
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async OpenGlobalTab(_topicID: string) {
      const existing = mockTabs.find((tab) => tab.scope === "global" && tab.topicId === _topicID);
      if (existing) {
        setMockActiveTab(existing.id);
        return { ...existing, active: true };
      }
      const tab: TabMeta = {
        id: "tab_" + Date.now(),
        scope: "global",
        workspaceRoot: "",
        workspaceName: "独立工作区",
        topicId: _topicID,
        topicTitle: topicLabel(_topicID, "独立工作区"),
        label: "deepseek-v4-flash",
        ready: true,
        running: false,
        mode: "normal",
        collaborationMode: "normal",
        toolApprovalMode: "ask",
        active: true,
        cwd: "",
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async OpenAutomationTab(_topicID: string) {
      const existing = mockTabs.find((tab) => tab.scope === "automation" && tab.topicId === _topicID);
      if (existing) {
        setMockActiveTab(existing.id);
        return { ...existing, active: true };
      }
      const tab: TabMeta = {
        id: "tab_" + Date.now(), scope: "automation", workspaceRoot: "", workspaceName: "Orca",
        topicId: _topicID, topicTitle: topicLabel(_topicID, "Orca"), label: "deepseek-v4-flash",
        ready: true, running: false, mode: "normal", collaborationMode: "normal", toolApprovalMode: "ask",
        promptMode: "assistant", enhancedModeEnabled: false, active: true, cwd: "",
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async EnsureBlankTab(scope: string, workspaceRoot: string) {
      const targetScope = scope === "automation" ? "automation" : scope === "project" && workspaceRoot ? "project" : "global";
      const targetRoot = targetScope === "project" ? workspaceRoot : "";
      const existing = mockTabs.find((tab) =>
        tab.scope === targetScope &&
        (targetScope === "global" || tab.workspaceRoot === targetRoot) &&
        !tab.running
      );
      if (existing) {
        setMockActiveTab(existing.id);
        return { ...existing, active: true };
      }
      const topic = await this.CreateTopic(targetScope, targetRoot, "");
      if (targetScope === "automation") return this.OpenAutomationTab(topic.id);
      return targetScope === "global" ? this.OpenGlobalTab(topic.id) : this.OpenProjectTab(targetRoot, topic.id);
    },
    async SetActiveTab(_tabID: string) {
      setMockActiveTab(_tabID);
      const tab = mockTabs.find((item) => item.id === _tabID);
      if (tab) queueMockTopicRuntime(tab);
    },
    async ReorderTabs(_tabIDs: string[]) {
      const byId = new Map(mockTabs.map((tab) => [tab.id, tab]));
      const ordered = _tabIDs.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      if (ordered.length === mockTabs.length) mockTabs = ordered;
    },
    async CloseTab(_tabID: string) {
      if (mockTabs.length <= 1) return;
      const wasActive = mockTabs.some((tab) => tab.id === _tabID && tab.active);
      mockTabs = mockTabs.filter((tab) => tab.id !== _tabID);
      if (wasActive && mockTabs.length > 0 && !mockTabs.some((tab) => tab.active)) {
        mockTabs[mockTabs.length - 1] = { ...mockTabs[mockTabs.length - 1], active: true };
      }
    },
    async ListProjectTree() {
      return cloneProjectTree();
    },
    async RenameProject(workspaceRoot: string, title: string) {
      const node = workspaceRoot
        ? mockProjectTree.find((item) => item.root === workspaceRoot)
        : mockProjectTree.find((item) => item.kind === "global_folder");
      if (node) node.label = title.trim() || (node.kind === "global_folder" ? "独立工作区" : node.label);
    },
    async SetProjectColor(workspaceRoot: string, color: string) {
      const node = workspaceRoot
        ? mockProjectTree.find((item) => item.root === workspaceRoot)
        : mockProjectTree.find((item) => item.kind === "global_folder");
      if (!node) return;
      node.projectColor = color || undefined;
      for (const child of projectChildren(node)) child.projectColor = node.projectColor;
      mockTabs = mockTabs.map((tab) =>
        (workspaceRoot ? tab.workspaceRoot === workspaceRoot : tab.scope === "global")
          ? { ...tab, projectColor: node.projectColor }
          : tab,
      );
    },
    async SetTopicPinned(topicID: string, pinned: boolean) {
      const topic = findMockTopic(topicID);
      if (topic) topic.pinned = pinned;
    },
    async ReorderProjects(workspaceRoots: string[]) {
      const projects = mockProjectTree.filter((node) => node.kind === "project");
      const globals = mockProjectTree.filter((node) => node.kind === "global_folder");
      if (!workspaceRoots.includes(GLOBAL_PROJECT_ORDER_KEY)) {
        if (workspaceRoots.length !== projects.length) return;
        const byRoot = new Map(projects.map((node) => [node.root, node]));
        const ordered = workspaceRoots.map((root) => byRoot.get(root)).filter((node): node is ProjectNode => Boolean(node));
        if (ordered.length !== projects.length) return;
        mockProjectTree.splice(0, mockProjectTree.length, ...globals, ...ordered);
        return;
      }
      const byKey = new Map<string, ProjectNode>();
      for (const node of projects) {
        if (node.root) byKey.set(node.root, node);
      }
      for (const node of globals) byKey.set(GLOBAL_PROJECT_ORDER_KEY, node);
      const seen = new Set<string>();
      const ordered: ProjectNode[] = [];
      for (const key of workspaceRoots) {
        if (seen.has(key)) return;
        const node = byKey.get(key);
        if (!node) return;
        seen.add(key);
        ordered.push(node);
      }
      if (ordered.length !== projects.length + globals.length) return;
      mockProjectTree.splice(0, mockProjectTree.length, ...ordered);
    },
    async CreateTopic(_scope: string, _workspaceRoot: string, title: string) {
      const now = Date.now();
      const id = "topic_" + now;
      const topicTitle = title.trim() || t("mock.newSession");
      const parent = _scope === "global"
        ? ensureMockGlobalFolder()
        : mockProjectTree.find((node) => node.root === _workspaceRoot);
      if (parent) {
        const global = parent.kind === "global_folder";
        parent.children = [{
          key: parent.kind === "global_folder" ? "global_topic_" + id : "topic_" + id,
          kind: global ? "global_topic" : "topic",
          label: topicTitle,
          root: parent.root,
          topicId: id,
          projectColor: parent.projectColor,
          createdAt: now,
        }, ...projectChildren(parent)];
      }
      return { id, title: topicTitle, createdAt: now };
    },
    async RenameTopic(topicID: string, title: string) {
      const topic = findMockTopic(topicID);
      const nextTitle = title.trim();
      if (!topic || !nextTitle) return;
      const activePrefix = topic.label?.startsWith("● ") ? "● " : "";
      topic.label = `${activePrefix}${nextTitle}`;
      mockTabs = mockTabs.map((tab) =>
        tab.topicId === topicID ? { ...tab, topicTitle: nextTitle } : tab,
      );
    },
    async GenerateTopicTitle(_scope: string, _workspaceRoot: string, topicID: string) {
      const title = "AI 生成标题";
      await this.RenameTopic(topicID, title);
      return title;
    },
    async DeleteTopic(topicID: string) {
      deleteMockTopic(topicID);
    },
    async TrashTopic(topicID: string) {
      deleteMockTopic(topicID);
    },
    async SaveWindowState(_state) {
      // no-op in browser dev — no real window geometry to persist
    },
    async ContextPanel(_tabID: string) {
      const now = Date.now();
      return {
        usedTokens: 42124,
        windowTokens: 128000,
        promptTokens: 22134,
        completionTokens: 12345,
        totalTokens: 34479,
        reasoningTokens: 7521,
        cacheHitTokens: 87000,
        cacheMissTokens: 13000,
        requestCount: 6,
        elapsedMs: 33 * 60 * 1000,
        sessionCost: 0.018,
        sessionCurrency: "¥",
        sessionCostUsd: 0.018,
        mock: true,
        readFiles: [
          { path: "README.md", turn: 2, time: now - 34 * 60 * 1000 },
          { path: "go.mod", turn: 3, time: now - 30 * 60 * 1000 },
          { path: "desktop/file.go", turn: 5, time: now - 13 * 60 * 1000, offset: 0, limit: 180 },
          { path: "internal/event.go", turn: 6, time: now - 4 * 60 * 1000, offset: 120, limit: 80, truncated: true },
        ],
        changedFiles: [
          { path: t("mock.changedFile1Path"), sources: ["session"], gitStatus: "modified", turns: [5, 6], latestPrompt: t("mock.changedFile1Prompt"), latestTime: now - 2 * 60 * 1000 },
          { path: t("mock.changedFile2Path"), sources: ["session"], gitStatus: "added", turns: [6], latestPrompt: t("mock.changedFile2Prompt"), latestTime: now - 60 * 1000 },
        ],
      };
    },
    async ListAutomations() {
      return mockAutomations.map((item) => ({ ...item }));
    },
    async PauseAutomation(id: string) {
      mockAutomations = mockAutomations.map((item) => item.id === id ? { ...item, status: "paused" } : item);
    },
    async ResumeAutomation(id: string) {
      mockAutomations = mockAutomations.map((item) => item.id === id ? { ...item, status: "scheduled", nextRunAt: new Date(Date.now() + 3_600_000).toISOString() } : item);
    },
    async CancelAutomation(id: string) {
      mockAutomations = mockAutomations.map((item) => item.id === id ? { ...item, status: "cancelled", nextRunAt: undefined } : item);
    },
    async ClearFinishedAutomations() {
      mockAutomations = mockAutomations.filter((item) => !["cancelled", "failed", "done"].includes(item.status));
    },
    async GetToolLibrarySettings() {
      return { ...mockToolLibrarySettings };
    },
    async SetToolLibrarySettings(settings: ToolLibrarySettings) {
      mockToolLibrarySettings = { ...settings };
    },
    async ListSideChat(tabID: string) {
      return (mockSideChats[tabID] ?? []).map((item) => ({ ...item }));
    },
    async SendSideChat(tabID: string, input: string) {
      const key = tabID || "active";
      const now = Date.now();
      const history = mockSideChats[key] ?? [];
      history.push({ id: `mock-side-user-${now}`, role: "user", content: input, createdAt: now });
      history.push({
        id: `mock-side-assistant-${now}`,
        role: "assistant",
        content: `我会只读参考主对话来回答。你刚问的是：${input}`,
        createdAt: now + 1,
      });
      mockSideChats[key] = history;
    },
    async ClearSideChat(tabID: string) {
      delete mockSideChats[tabID || "active"];
    },
    async CancelSideChat(_tabID: string) {},
  };
}

