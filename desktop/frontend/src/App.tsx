import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from "react";
import { ShellExpandProvider, useShellExpand } from "./lib/shellExpand";
import {
  Activity,
  Command,
  Download,
  SquarePen,
  FileDown,
  FileImage,
  FileText,
  FileJson,
  GitBranch,
  History,
  MessageSquareText,
  MoreHorizontal,
  Settings as SettingsIcon,
  Pencil,
  Trash2,
} from "lucide-react";
import { useToast } from "./lib/toast";
import { asArray } from "./lib/array";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, useI18n, useT } from "./lib/i18n";
import { useController, type Item, type LiveStream } from "./lib/useController";
import { app, onProjectTreeChanged, openExternal } from "./lib/bridge";
import { Transcript } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { ClearContextCard } from "./components/ClearContextCard";
import { StatusBar } from "./components/StatusBar";
import { HistoryPanel } from "./components/HistoryPanel";
import { CommandPalette, type PaletteItem } from "./components/CommandPalette";
import { SettingsPanel } from "./components/SettingsPanel";
import { ContextPanel } from "./components/ContextPanel";
import { WorkspacePanel } from "./components/WorkspacePanel";
import { Tooltip } from "./components/Tooltip";
import { StartupSplash, shouldShowStartupSplash } from "./components/StartupSplash";
import { OnboardingOverlay } from "./components/OnboardingOverlay";
import { AppChrome } from "./components/AppChrome";
import { ProjectTree } from "./components/ProjectTree";
import { NewSessionChooser } from "./components/NewSessionChooser";
import { CopyButton } from "./components/CopyButton";
import { UpdateBanner } from "./components/UpdateBanner";
import { AutomationPanel } from "./components/AutomationPanel";
import { ToolLibraryPanel } from "./components/ToolLibraryPanel";
import { SideChatPanel } from "./components/SideChatPanel";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./components/ContextMenu";
import { parseTodos } from "./lib/tools";
import { shouldShowTodoPanel } from "./lib/todoVisibility";
import {
  modeHasAutoApproveTools,
  modeHasPlan,
  modeFromAxes,
  normalizeMode,
  normalizeToolApprovalMode,
  type CollaborationMode,
  type ComposerInsertRequest,
  type Mode,
  type PromptMode,
  type ProcessDisplayMode,
  type ProductCapabilities,
  type SessionMeta,
  type SettingsTab,
  type SettingsView,
  type TabMeta,
  type ToolApprovalMode,
  type UpdateInfo,
} from "./lib/types";
import { checkDesktopUpdate, UPDATE_AVAILABLE_EVENT, UPDATE_CHECK_INTERVAL_MS } from "./lib/updateCheck";
import { acceptUpdate, dismissUpdate, reopenUpdate, shouldShowUpdate } from "./lib/updaterBannerState";
import {
  controllerCollaborationMode,
  displayedCollaborationMode,
  keepGoalDraftMode,
  metaSyncedCollaborationMode,
  tabListCollaborationMode,
} from "./lib/goalDraftMode";
import {
  restorableToolApprovalMode,
  toggleYoloToolApprovalMode,
  type RestorableToolApprovalMode,
} from "./lib/toolApprovalMode";
import { loadLayoutSize, saveLayoutSize } from "./lib/layoutPreferences";
import { blobToBase64, renderSessionImageBlob, renderSessionPdfBlob } from "./lib/sessionExport";
import {
  applyTheme,
  clearLegacyThemePreference,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  readLegacyThemePreference,
} from "./lib/theme";
import { applyTextSize, DEFAULT_TEXT_SIZE, getTextSize, nextTextSize } from "./lib/textSize";
import { applyUIStyle, getUIStyle, type UIStyle } from "./lib/uiStyle";
import { useWindowStatePersistence } from "./lib/windowState";
import { availableWorkspacePanelWidth, resolveWorkspacePanelWidth, workspacePanelAriaMinWidth } from "./lib/workspaceLayout";
import logoSymbol from "./assets/logo-symbol.png";

const SIDEBAR_COLLAPSED_KEY = "orca.sidebar.collapsed";
const SIDEBAR_DEFAULT_WIDTH = 264;
const SIDEBAR_MIN_WIDTH = 264;
const SIDEBAR_MAX_WIDTH = 300;
const SIDEBAR_VIEWPORT_RATIO = 0.18;
const CHAT_MIN_WIDTH = 400;
const CHAT_COMFORT_MIN_WIDTH = 560;
const WORKSPACE_RESIZER_WIDTH = 8;

const RIGHT_DOCK_TREE_DEFAULT_WIDTH = 300;
const RIGHT_DOCK_TREE_MIN_WIDTH = 300;
const RIGHT_DOCK_TREE_MAX_WIDTH = 560;
const RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH = 660;
const RIGHT_DOCK_PREVIEW_MIN_WIDTH = 420;
const RIGHT_DOCK_MIN_RENDER_WIDTH = 280;
const RIGHT_DOCK_MAX_WIDTH = 860;

type RightDockMode = "context" | "files" | "changed" | "sideChat";
type WorkspaceRevealRequest = { id: number; path: string };
type WorkspaceFileListRequest = { id: number; paths: string[] };
type WorkspaceChangeListEntry = { key: string; path: string; meta: string; time: string; detail: string };
type WorkspaceChangeListRequest = { id: number; changes: WorkspaceChangeListEntry[] };
type QueuedPrompt = { id: string; displayText: string; submitText: string; guided?: boolean };
const SHOW_CONTEXT_DOCK = true;
const INDEPENDENT_WORKSPACE_TITLE = "独立工作区";
type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
type DesktopPlatform = "darwin" | "windows" | "linux";
type HistoryViewState =
  | { kind: "history"; source: "scope"; filter: HistoryScopeFilter; sessions: SessionMeta[] }
  | { kind: "history"; source: "all"; sessions: SessionMeta[] }
  | { kind: "trash"; sessions: SessionMeta[] };

const ENGINEERING_CAPABILITIES: ProductCapabilities = {
  edition: "engineering",
	promptModes: ["assistant", "coding"],
	conversationModes: ["assistant", "coding"],
	assistantMemoryEnabled: true,
  automationWorkspaceEnabled: true,
	orcaEnabled: true,
};

function normalizePromptMode(mode?: string, _enhanced?: boolean, supported: PromptMode[] = ENGINEERING_CAPABILITIES.promptModes): PromptMode {
	const candidate: PromptMode = mode === "assistant" ? "assistant" : "coding";
  if (supported.includes(candidate)) return candidate;
	return supported[0] ?? "assistant";
}

function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

function clampRightDockPreviewWidth(width: number): number {
  return Math.min(RIGHT_DOCK_MAX_WIDTH, Math.max(RIGHT_DOCK_PREVIEW_MIN_WIDTH, Math.round(width)));
}

function clampRightDockTreeWidth(width: number): number {
  return Math.min(RIGHT_DOCK_TREE_MAX_WIDTH, Math.max(RIGHT_DOCK_TREE_MIN_WIDTH, Math.round(width)));
}

function defaultSidebarWidth(): number {
  if (typeof window !== "undefined") {
    return clampSidebarWidth(window.innerWidth * SIDEBAR_VIEWPORT_RATIO);
  }
  return SIDEBAR_DEFAULT_WIDTH;
}

function defaultRightDockTreeWidth(): number {
  return RIGHT_DOCK_TREE_DEFAULT_WIDTH;
}

function loadSidebarCollapsed(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

function saveSidebarCollapsed(collapsed: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? "1" : "0");
  } catch {
    /* ignore storage failures */
  }
}

function loadSidebarWidth(): number {
  return loadLayoutSize("sidebarWidthGraphite", defaultSidebarWidth(), clampSidebarWidth);
}

function saveSidebarWidth(width: number): void {
  saveLayoutSize("sidebarWidthGraphite", width, clampSidebarWidth);
}

type TextEditableElement = HTMLInputElement | HTMLTextAreaElement | HTMLElement;

function isTextInputElement(el: Element | null): el is HTMLInputElement {
  if (!(el instanceof HTMLInputElement)) return false;
  const nonTextTypes = new Set([
    "button",
    "checkbox",
    "color",
    "file",
    "hidden",
    "image",
    "radio",
    "range",
    "reset",
    "submit",
  ]);
  return !nonTextTypes.has(el.type);
}

function editableElementFromTarget(target: EventTarget | null): TextEditableElement | null {
  const el = target instanceof Element ? target : null;
  if (!el) return null;
  const input = el.closest("input");
  if (isTextInputElement(input)) return input;
  const textarea = el.closest("textarea");
  if (textarea instanceof HTMLTextAreaElement) return textarea;
  for (let node: Element | null = el; node; node = node.parentElement) {
    if (node instanceof HTMLElement && node.isContentEditable) return node;
  }
  return null;
}

function textSelectionForEditable(el: TextEditableElement): string {
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
    const start = el.selectionStart ?? 0;
    const end = el.selectionEnd ?? start;
    return el.value.slice(Math.min(start, end), Math.max(start, end));
  }
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || !selection.toString()) return "";
  return el.contains(selection.anchorNode) && el.contains(selection.focusNode) ? selection.toString() : "";
}

function editableHasText(el: TextEditableElement): boolean {
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return el.value.length > 0;
  return (el.textContent ?? "").length > 0;
}

function canMutateEditable(el: TextEditableElement): boolean {
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return !el.disabled && !el.readOnly;
  return el.isContentEditable;
}

function setNativeEditableValue(el: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, "value")?.set;
  setter?.call(el, value);
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

function replaceEditableSelection(el: TextEditableElement, inserted: string): void {
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
    const start = el.selectionStart ?? el.value.length;
    const end = el.selectionEnd ?? start;
    const safeStart = Math.min(start, end);
    const safeEnd = Math.max(start, end);
    const next = el.value.slice(0, safeStart) + inserted + el.value.slice(safeEnd);
    const pos = safeStart + inserted.length;
    setNativeEditableValue(el, next);
    requestAnimationFrame(() => {
      el.focus();
      try {
        el.selectionStart = el.selectionEnd = pos;
      } catch {
        /* selection APIs are unavailable for a few input types */
      }
    });
    return;
  }
  el.focus();
  document.execCommand("insertText", false, inserted);
}

function selectAllEditable(el: TextEditableElement): void {
  el.focus();
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
    el.selectionStart = 0;
    el.selectionEnd = el.value.length;
    return;
  }
  const range = document.createRange();
  range.selectNodeContents(el);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}

function normalizeDesktopPlatform(value: string): DesktopPlatform {
  if (value === "darwin" || value === "windows") return value;
  return "linux";
}

function browserPlatformOverride(): DesktopPlatform | null {
  if (typeof window === "undefined" || window.runtime) return null;
  const value = new URLSearchParams(window.location.search).get("platform");
  if (value === "darwin" || value === "windows" || value === "linux") return value;
  return null;
}

function detectBrowserPlatform(): DesktopPlatform {
  const override = browserPlatformOverride();
  if (override) return override;
  if (typeof navigator === "undefined") return "linux";
  const marker = `${navigator.platform} ${navigator.userAgent}`;
  if (/Win/i.test(marker)) return "windows";
  if (/Mac/i.test(marker)) return "darwin";
  return "linux";
}

function loadRightDockTreeWidth(): number {
  return loadLayoutSize("rightDockTreeWidth", defaultRightDockTreeWidth(), clampRightDockTreeWidth);
}

function saveRightDockTreeWidth(width: number): void {
  saveLayoutSize("rightDockTreeWidth", width, clampRightDockTreeWidth);
}

function loadRightDockPreviewWidth(): number {
  return loadLayoutSize("rightDockPreviewWidth", RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH, clampRightDockPreviewWidth);
}

function saveRightDockPreviewWidth(width: number): void {
  saveLayoutSize("rightDockPreviewWidth", width, clampRightDockPreviewWidth);
}

function tabWorkspaceTitle(tab?: TabMeta): string {
  if (!tab) return INDEPENDENT_WORKSPACE_TITLE;
  if (tab.scope === "project") return tab.workspaceName || tab.workspaceRoot || "Project";
  if (tab.scope === "global") return tab.workspaceName && tab.workspaceName !== "Global" ? tab.workspaceName : INDEPENDENT_WORKSPACE_TITLE;
  return tab.workspaceName || tab.workspaceRoot || INDEPENDENT_WORKSPACE_TITLE;
}

function topicTitle(tab?: TabMeta): string {
  if (!tab) return INDEPENDENT_WORKSPACE_TITLE;
  const workspaceTitle = tabWorkspaceTitle(tab);
  const topic = tab.topicTitle && !(tab.scope === "global" && tab.topicTitle === "Global") ? tab.topicTitle : (tab.scope === "global" ? workspaceTitle : "Untitled");
  return topic === workspaceTitle ? workspaceTitle : `${workspaceTitle} / ${topic}`;
}

function topicDisplayTitle(tab?: TabMeta): string {
  if (!tab) return INDEPENDENT_WORKSPACE_TITLE;
  if (tab.scope === "global" && (!tab.topicTitle || tab.topicTitle === "Global")) return tabWorkspaceTitle(tab);
  return tab.topicTitle || (tab.scope === "global" ? tabWorkspaceTitle(tab) : "Untitled");
}

function sessionsForScope(sessions: SessionMeta[], filter: HistoryScopeFilter): SessionMeta[] {
  if (filter.scope === "project") {
    return sessions.filter((session) => session.scope === "project" && session.workspaceRoot === filter.workspaceRoot);
  }
  return sessions.filter((session) => (session.scope || "global") === "global");
}

function workspaceDisplayName(path?: string): string {
  if (!path) return "";
  const parts = path.split(/[/\\]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
}

function materializeLiveItems(items: Item[], live?: LiveStream): Item[] {
  if (!live) return items;
  return items.map((item) => {
    if (item.kind !== "assistant" || item.id !== live.id) return item;
    return { ...item, text: live.text, reasoning: live.reasoning, streaming: true };
  });
}

function fence(label: string, value: string): string {
  if (!value.trim()) return "";
  const fenceToken = value.includes("```") ? "````" : "```";
  return `${label}\n${fenceToken}\n${value.trim()}\n${fenceToken}`;
}

function sessionItemsToMarkdown(title: string, items: Item[], live?: LiveStream): string {
  const lines: string[] = [`# ${title.trim() || "O.R.C.A. session"}`, ""];
  for (const item of materializeLiveItems(items, live)) {
    switch (item.kind) {
      case "user":
        lines.push("## User", "", item.text.trim(), "");
        break;
      case "assistant":
        lines.push("## Assistant");
        if (item.reasoning.trim()) {
          lines.push("", "### Reasoning", "", item.reasoning.trim());
        }
        if (item.text.trim()) {
          lines.push("", item.text.trim());
        }
        lines.push("");
        break;
      case "tool":
        lines.push(`### Tool: ${item.name}`);
        if (item.args.trim()) lines.push("", fence("Args", item.args));
        if (item.output?.trim()) lines.push("", fence("Output", item.output));
        if (item.error?.trim()) lines.push("", fence("Error", item.error));
        lines.push("");
        break;
      case "phase":
        lines.push(`### Phase`, "", item.text.trim(), "");
        break;
      case "notice":
        lines.push(`### ${item.level === "warn" ? "Warning" : "Notice"}`, "", item.text.trim(), "");
        break;
      case "compaction":
        lines.push("### Context Compaction", "");
        if (item.pending) {
          lines.push("Compaction pending.");
        } else {
          lines.push(`Messages: ${item.messages}`);
          if (item.trigger) lines.push(`Trigger: ${item.trigger}`);
          if (item.summary.trim()) lines.push("", item.summary.trim());
        }
        lines.push("");
        break;
    }
  }
  return lines.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd() + "\n";
}

function sessionItemsToJson(title: string, items: Item[], live?: LiveStream): string {
  return JSON.stringify(
    {
      title,
      exportedAt: new Date().toISOString(),
      items: materializeLiveItems(items, live),
    },
    null,
    2,
  );
}

function safeFilename(name: string): string {
  const cleaned = name.trim().replace(/[\\/:*?"<>|]+/g, "-").replace(/\s+/g, " ").slice(0, 80);
  return cleaned || "orca-session";
}

function modelDisplayLabel(ref: string, displayLabel?: string): string {
  const label = displayLabel?.trim();
  if (label) return label;
  const parts = ref.split("/").filter(Boolean);
  return parts[parts.length - 1] || ref;
}

/** Global hotkey handler for shell-expand toggle (Ctrl/Cmd+B). */
function ShellHotkeys() {
  const shellExpand = useShellExpand();
  useEffect(() => {
    if (!shellExpand) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "b") {
        e.preventDefault();
        shellExpand.toggleLast();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [shellExpand]);
  return null;
}

/** Global hotkey handler for text-size shortcuts (Ctrl/Cmd + Plus/Minus/0). */
function TextSizeHotkeys() {
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      if (e.key !== "+" && e.key !== "=" && e.key !== "-" && e.key !== "0") return;

      e.preventDefault();
      if (e.key === "0") {
        applyTextSize(DEFAULT_TEXT_SIZE);
        return;
      }
      applyTextSize(nextTextSize(getTextSize(), e.key === "-" ? -1 : 1));
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);
  return null;
}

export default function App() {
  const {
    state,
    activeTabId,
    send,
    runShell,
    steer,
    notice,
    cancel,
    approve,
    answerQuestion,
    setControllerMode,
    setCollaborationMode: setControllerCollaborationMode,
    setToolApprovalMode: setControllerToolApprovalMode,
    setAskWorkflow: setControllerAskWorkflow,
    setStepThinking: setControllerStepThinking,
    togglePause,
    setGoal: setControllerGoal,
    clearGoal: clearControllerGoal,
    clearSession,
    listSessions,
    listTrashedSessions,
    resumeSession,
    previewSession,
    deleteSession,
    restoreSession,
    purgeTrashedSession,
    renameSession,
    refreshMeta,
    pickWorkspace,
    switchWorkspace,
    rewind,
    setModel,
    setEffort,
    switchTab,
    openProjectTab,
    openGlobalTab,
    openAutomationTab,
    syncActiveTab,
    ensureBlankTab,
    closeTab,
  } = useController();
  const { locale, setPref: setLocalePref } = useI18n();
  const t = useT();
  const [modesByTab, setModesByTab] = useState<Record<string, Mode>>({});
  const [collaborationModesByTab, setCollaborationModesByTab] = useState<Record<string, CollaborationMode>>({});
  const [toolApprovalModesByTab, setToolApprovalModesByTab] = useState<Record<string, ToolApprovalMode>>({});
  const [askWorkflowsByTab, setAskWorkflowsByTab] = useState<Record<string, boolean>>({});
  const [stepThinkingsByTab, setStepThinkingsByTab] = useState<Record<string, boolean>>({});
  const [promptModesByTab, setPromptModesByTab] = useState<Record<string, PromptMode>>({});
  const [productCapabilities, setProductCapabilities] = useState<ProductCapabilities>(ENGINEERING_CAPABILITIES);
  const [promptModeSwitchingByTab, setPromptModeSwitchingByTab] = useState<Record<string, boolean>>({});
  const [promptModeSwitchFailedByTab, setPromptModeSwitchFailedByTab] = useState<Record<string, boolean>>({});
  const [pendingModelLabelsByTab, setPendingModelLabelsByTab] = useState<Record<string, string>>({});
  const [pendingEffortsByTab, setPendingEffortsByTab] = useState<Record<string, string>>({});
  const [pendingPromptModesByTab, setPendingPromptModesByTab] = useState<Record<string, PromptMode>>({});
  const yoloRestoreToolApprovalModesRef = useRef<Record<string, RestorableToolApprovalMode>>({});
  const [goalsByTab, setGoalsByTab] = useState<Record<string, string>>({});
  const [goalDraftModesByTab, setGoalDraftModesByTab] = useState<Record<string, boolean>>({});
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);
  const [pendingTopic, setPendingTopic] = useState<{ scope: string; workspaceRoot: string; topicId: string } | null>(null);
  const [startupSplashVisible, setStartupSplashVisible] = useState<boolean>(() => shouldShowStartupSplash());
  // null until the mount probe resolves; true shows the overlay. Probed once —
  // clearing the key mid-session is the Settings panel's job, not the gate's.
  const [needsOnboarding, setNeedsOnboarding] = useState<boolean | null>(null);
  const [settingsTarget, setSettingsTarget] = useState<SettingsTab | null>(null);
	const [desktopUIStyle, setDesktopUIStyle] = useState<UIStyle>(getUIStyle);
	const startupStyleSyncedRef = useRef(false);
	const activeUIStyleRef = useRef<UIStyle>(getUIStyle());
  const [checkUpdatesEnabled, setCheckUpdatesEnabled] = useState<boolean | null>(null);
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [updateDismissal, setUpdateDismissal] = useState({ version: "", dismissed: false });
  const [automationPanelOpen, setAutomationPanelOpen] = useState(false);
  const [toolLibraryPanelOpen, setToolLibraryPanelOpen] = useState(false);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [newSessionChooserOpen, setNewSessionChooserOpen] = useState(false);
  const [paletteSessions, setPaletteSessions] = useState<SessionMeta[]>([]);
  const { showToast } = useToast();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(loadSidebarCollapsed);
  const [sidebarWidth, setSidebarWidth] = useState(loadSidebarWidth);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [viewportWidth, setViewportWidth] = useState(() => (typeof window === "undefined" ? 1440 : window.innerWidth));
  const [workspacePanelOpen, setWorkspacePanelOpen] = useState(true);
  const [responsiveLayoutCompact, setResponsiveLayoutCompact] = useState(false);
  const [rightDockTreeWidth, setRightDockTreeWidth] = useState(loadRightDockTreeWidth);
  const [rightDockPreviewWidth, setRightDockPreviewWidth] = useState(loadRightDockPreviewWidth);
  const [workspacePreviewActive, setWorkspacePreviewActive] = useState(false);
  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [workspacePanelMaximized, setWorkspacePanelMaximized] = useState(false);
  const [rightDockMode, setRightDockMode] = useState<RightDockMode>("context");
  const [workspaceRevealRequest, setWorkspaceRevealRequest] = useState<WorkspaceRevealRequest | null>(null);
  const [workspaceChangeRevealRequest, setWorkspaceChangeRevealRequest] = useState<WorkspaceRevealRequest | null>(null);
  const [workspaceFileListRequest, setWorkspaceFileListRequest] = useState<WorkspaceFileListRequest | null>(null);
  const [workspaceChangeListRequest, setWorkspaceChangeListRequest] = useState<WorkspaceChangeListRequest | null>(null);
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [projectRevision, setProjectRevision] = useState(0);
  const [composerInsertRequest, setComposerInsertRequest] = useState<ComposerInsertRequest | null>(null);
  const [composerPasteRequest, setComposerPasteRequest] = useState(0);
  const [transientOverlayDismissSignal, setTransientOverlayDismissSignal] = useState(0);
  const [desktopPlatform, setDesktopPlatform] = useState<DesktopPlatform>(detectBrowserPlatform);
  const [processDisplayMode, setProcessDisplayMode] = useState<ProcessDisplayMode>("compact");
  const [activityIndicatorEnabled, setActivityIndicatorEnabled] = useState(true);
  const [renamingTopicId, setRenamingTopicId] = useState<string | null>(null);
  const [topicTitleDraft, setTopicTitleDraft] = useState("");
  const [topicExportOpen, setTopicExportOpen] = useState(false);
  const [topicOverflowOpen, setTopicOverflowOpen] = useState(false);
  const [sidebarTogglePressed, setSidebarTogglePressed] = useState(false);
  const [workspaceTogglePressed, setWorkspaceTogglePressed] = useState(false);
  const [clearContextPending, setClearContextPending] = useState(false);
  const [textEditMenuPoint, setTextEditMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [textEditTarget, setTextEditTarget] = useState<TextEditableElement | null>(null);
  const [textEditSelection, setTextEditSelection] = useState("");
  const [selectionMenuPoint, setSelectionMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [selectionMenuText, setSelectionMenuText] = useState("");
  const [queuedPromptsByTab, setQueuedPromptsByTab] = useState<Record<string, QueuedPrompt[]>>({});
  const topicRenameSkipCommitRef = useRef(false);
  const topicRenameCommitHandledRef = useRef(false);
  const nextQueuedPromptIdRef = useRef(1);
  const queuedPromptDispatchingRef = useRef(false);
  const tabMetasSignatureRef = useRef("");
  const promptModeSwitchingRef = useRef<Record<string, boolean>>({});
  const pendingModelSwitchRef = useRef<Record<string, Promise<void>>>({});
  const pendingEffortSwitchRef = useRef<Record<string, Promise<void>>>({});
  const pendingPromptModeSwitchRef = useRef<Partial<Record<string, Promise<void>>>>({});
  const pendingPromptModeOriginRef = useRef<Record<string, PromptMode>>({});
  const latestModelSwitchRef = useRef<Record<string, string>>({});
  const latestEffortSwitchRef = useRef<Record<string, string>>({});
  const latestPromptModeSwitchRef = useRef<Record<string, PromptMode>>({});
  const appRef = useRef<HTMLDivElement>(null);
  const sidebarTogglePressTimerRef = useRef<number | null>(null);
  const workspaceTogglePressTimerRef = useRef<number | null>(null);

  // Persist window geometry across launches.
  useWindowStatePersistence();

  const closeTransientOverlays = useCallback(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  }, []);

  const pulseSidebarToggle = useCallback(() => {
    if (typeof window === "undefined") return;
    if (sidebarTogglePressTimerRef.current !== null) {
      window.clearTimeout(sidebarTogglePressTimerRef.current);
    }
    setSidebarTogglePressed(true);
    sidebarTogglePressTimerRef.current = window.setTimeout(() => {
      sidebarTogglePressTimerRef.current = null;
      setSidebarTogglePressed(false);
    }, 260);
  }, []);

  const pulseWorkspaceToggle = useCallback(() => {
    if (typeof window === "undefined") return;
    if (workspaceTogglePressTimerRef.current !== null) {
      window.clearTimeout(workspaceTogglePressTimerRef.current);
    }
    setWorkspaceTogglePressed(true);
    workspaceTogglePressTimerRef.current = window.setTimeout(() => {
      workspaceTogglePressTimerRef.current = null;
      setWorkspaceTogglePressed(false);
    }, 260);
  }, []);

  const anchorAppScrollToChat = useCallback(() => {
    if (typeof window === "undefined") return;
    const el = appRef.current;
    if (!el) return;
    const pin = () => {
      el.scrollLeft = 0;
    };
    pin();
    window.requestAnimationFrame(pin);
    window.setTimeout(pin, 300);
  }, []);

  useEffect(() => {
    return () => {
      if (sidebarTogglePressTimerRef.current !== null) {
        window.clearTimeout(sidebarTogglePressTimerRef.current);
      }
      if (workspaceTogglePressTimerRef.current !== null) {
        window.clearTimeout(workspaceTogglePressTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    const override = browserPlatformOverride();
    if (override) {
      setDesktopPlatform(override);
      return () => {
        cancelled = true;
      };
    }
    void app.Platform()
      .then((value) => {
        if (!cancelled) setDesktopPlatform(normalizeDesktopPlatform(value));
      })
      .catch((e) => {
        console.warn("platform probe failed", e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    void app.GetProductCapabilities()
      .then((capabilities) => {
        if (cancelled) return;
		const promptModes = (capabilities.conversationModes ?? capabilities.promptModes).filter(
		  (mode): mode is PromptMode => mode === "assistant" || mode === "coding",
        );
        setProductCapabilities({
          ...capabilities,
          promptModes: promptModes.length > 0 ? promptModes : ENGINEERING_CAPABILITIES.promptModes,
        });
      })
      .catch((e) => console.warn("product capability probe failed", e));
    return () => {
      cancelled = true;
    };
  }, []);

  const applyDesktopPreferences = useCallback(
    (settings: Pick<SettingsView, "desktopTheme" | "desktopThemeStyle" | "desktopUIStyle" | "desktopLanguage" | "processDisplayMode" | "expandThinking" | "activityIndicatorEnabled" | "checkUpdates">) => {
	  const firstSync = !startupStyleSyncedRef.current;
	  const runtimeStyle: UIStyle = firstSync
		? settings.desktopUIStyle === "classic" ? "classic" : "modern"
		: activeUIStyleRef.current;
	  if (runtimeStyle === "classic") {
		const theme = normalizeThemePreference(settings.desktopTheme);
		applyTheme(theme, normalizeThemeStyleForTheme(settings.desktopThemeStyle, theme), { persist: false });
	  } else {
		applyTheme("light", "slate", { persist: false });
	  }
	  // Re-assert the active window branch after applyTheme. Classic removes
	  // data-theme-style; Modern pins it to slate. A saved style change remains
	  // pending until restart and cannot partially restyle the live window.
	  applyUIStyle(runtimeStyle, { persist: false });
	  // The persisted config is authoritative at startup. Later settings saves
	  // intentionally do not switch the current DOM because the window frame is
	  // fixed for the lifetime of this process.
	  if (firstSync) {
		startupStyleSyncedRef.current = true;
		activeUIStyleRef.current = runtimeStyle;
		setDesktopUIStyle(runtimeStyle);
	  }
      setLocalePref(normalizeLangPref(settings.desktopLanguage));
      setProcessDisplayMode(settings.processDisplayMode === "compact" || settings.processDisplayMode === "detailed"
        ? settings.processDisplayMode
        : settings.expandThinking ? "detailed" : "compact");
      setActivityIndicatorEnabled(Boolean(settings.activityIndicatorEnabled));
      setCheckUpdatesEnabled(settings.checkUpdates !== false);
    },
    [setLocalePref],
  );

  useEffect(() => {
    let cancelled = false;
    const syncDesktopPreferences = async () => {
      const legacyLanguage = readLegacyLangPref();
      const legacyTheme = readLegacyThemePreference();
      if (legacyLanguage) {
        await app.MigrateDesktopPreferences(legacyLanguage, "light", "slate");
        clearLegacyLangPref();
      }
      if (legacyTheme.hasValue) {
        clearLegacyThemePreference();
      }
      const settings = await app.Settings();
      if (cancelled) return;
      applyDesktopPreferences(settings);
    };
    void syncDesktopPreferences().catch((e) => {
      console.warn("desktop preferences sync failed", e);
    });
    return () => {
      cancelled = true;
    };
  }, [applyDesktopPreferences]);

  useEffect(() => {
    const acceptAvailableUpdate = (info: UpdateInfo) => {
      setUpdateInfo(info);
      setUpdateDismissal((current) => acceptUpdate(current, info.latest));
    };
    const onUpdate = (event: Event) => {
      const detail = (event as CustomEvent<UpdateInfo>).detail;
      if (detail?.available) acceptAvailableUpdate(detail);
    };
    window.addEventListener(UPDATE_AVAILABLE_EVENT, onUpdate);
    return () => window.removeEventListener(UPDATE_AVAILABLE_EVENT, onUpdate);
  }, []);

  useEffect(() => {
    if (!checkUpdatesEnabled) {
      setUpdateInfo(null);
      setUpdateDismissal({ version: "", dismissed: false });
      return undefined;
    }
    let cancelled = false;
    const check = async () => {
      try {
        const currentVersion = await app.Version();
        const info = await checkDesktopUpdate(currentVersion);
        if (!cancelled && info?.available) {
          setUpdateInfo(info);
          setUpdateDismissal((current) => acceptUpdate(current, info.latest));
        }
      } catch {
        // Automatic checks are intentionally silent.
      }
    };
    const initialTimer = window.setTimeout(() => void check(), 10_000);
    const interval = window.setInterval(() => void check(), UPDATE_CHECK_INTERVAL_MS);
    return () => {
      cancelled = true;
      window.clearTimeout(initialTimer);
      window.clearInterval(interval);
    };
  }, [checkUpdatesEnabled]);

  const openUpdatePage = useCallback(() => {
    if (updateInfo && !updateInfo.canDownload) {
      if (updateInfo.downloadUrl) openExternal(updateInfo.downloadUrl);
      else void app.OpenDownloadPage();
      return;
    }
    if (updateInfo && !shouldShowUpdate(updateDismissal, updateInfo.latest)) {
      setUpdateDismissal((current) => reopenUpdate(current, updateInfo.latest));
      return;
    }
    const banner = document.querySelector<HTMLElement>(".orca-update-banner");
    if (banner) {
      banner.focus({ preventScroll: false });
      return;
    }
    if (updateInfo?.downloadUrl) {
      openExternal(updateInfo.downloadUrl);
      return;
    }
    void app.OpenDownloadPage();
  }, [updateDismissal, updateInfo]);

  // Open settings when the native menu item (CmdOrCtrl+,) is activated.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      closeTransientOverlays();
      setSettingsTarget("general");
    });
  }, [closeTransientOverlays]);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onResize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  useEffect(() => {
    const compact = viewportWidth < 1120;
    setResponsiveLayoutCompact((current) => (current === compact ? current : compact));
  }, [viewportWidth]);
  const [pendingPlanRevision, setPendingPlanRevision] = useState<string | null>(null);
  const [footerHeight, setFooterHeight] = useState(0);
  const footerHeightRef = useRef(0);
  const footerRef = useRef<HTMLElement>(null);
  const runningRef = useRef(state.running);
  const collaborationModeRef = useRef<CollaborationMode>("normal");
  const toolApprovalModeRef = useRef<ToolApprovalMode>("ask");
  const askWorkflowEnabledRef = useRef(false);
  const stepThinkingEnabledRef = useRef(false);
  const goalRef = useRef("");
  const rightDockDetailActive = rightDockMode !== "context" && workspacePreviewActive;
  const preferredWorkspacePanelWidth = rightDockDetailActive ? rightDockPreviewWidth : rightDockTreeWidth;
  const workspacePanelMinWidth = rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;
  const responsiveSidebarCollapsed = responsiveLayoutCompact || sidebarCollapsed;
  const responsiveWorkspacePanelOpen = workspacePanelOpen && !responsiveLayoutCompact;
  const chatReservedWidth = responsiveWorkspacePanelOpen && !workspacePanelMaximized ? CHAT_COMFORT_MIN_WIDTH : CHAT_MIN_WIDTH;
  const workspacePanelAvailableWidth = availableWorkspacePanelWidth({
    viewportWidth,
    sidebarCollapsed: responsiveSidebarCollapsed,
    sidebarWidth,
    chatMinWidth: chatReservedWidth,
    resizerWidth: WORKSPACE_RESIZER_WIDTH,
  });

  const resolvedWorkspacePanelWidth = resolveWorkspacePanelWidth({
    open: responsiveWorkspacePanelOpen,
    maximized: workspacePanelMaximized,
    preferredWidth: preferredWorkspacePanelWidth,
    minWidth: workspacePanelMinWidth,
    availableWidth: workspacePanelAvailableWidth,
  });

  const workspacePanelRenderable =
    responsiveWorkspacePanelOpen && (workspacePanelMaximized || resolvedWorkspacePanelWidth >= RIGHT_DOCK_MIN_RENDER_WIDTH);
  const workspacePanelGridOpen = workspacePanelRenderable && !workspacePanelMaximized;
  const workspacePanelRenderWidth = workspacePanelMaximized ? preferredWorkspacePanelWidth : resolvedWorkspacePanelWidth;
  const activeTab = useMemo(
    () => tabMetas.find((tab) => tab.id === activeTabId) ?? tabMetas.find((tab) => tab.active),
    [activeTabId, tabMetas],
  );
  const activeQueuedPrompts = activeTabId ? queuedPromptsByTab[activeTabId] ?? [] : [];
  const startupSplashHold = state.meta?.ready !== true && !state.meta?.startupErr;
  const legacyMode = activeTabId ? modesByTab[activeTabId] ?? "normal" : "normal";
  const goal = activeTabId ? goalsByTab[activeTabId] ?? state.meta?.goal ?? activeTab?.goal ?? "" : "";
  const goalDraftMode = activeTabId ? Boolean(goalDraftModesByTab[activeTabId]) : false;
  const collaborationMode = activeTabId
    ? displayedCollaborationMode({
        goalDraftMode,
        localMode: collaborationModesByTab[activeTabId],
        metaGoal: state.meta?.goal,
        tabMode: activeTab?.collaborationMode,
        goal,
        legacyMode,
      })
	: "normal";
  const toolApprovalMode = activeTabId
    ? toolApprovalModesByTab[activeTabId] ?? normalizeToolApprovalMode(state.meta?.toolApprovalMode ?? activeTab?.toolApprovalMode, legacyMode, state.meta?.autoApproveTools ?? state.meta?.bypass)
    : "ask";
  const askWorkflowEnabled = activeTabId
    ? askWorkflowsByTab[activeTabId] ?? Boolean(state.meta?.askWorkflowEnabled ?? activeTab?.askWorkflowEnabled)
    : false;
  const stepThinkingEnabled = activeTabId
    ? stepThinkingsByTab[activeTabId] ?? Boolean(state.meta?.stepThinkingEnabled ?? activeTab?.stepThinkingEnabled)
    : false;
  const automationConversation = (state.meta?.scope ?? activeTab?.scope) === "automation";
  const automationAccessRequired = automationConversation && state.meta?.automationFullAccessApproved !== true && !state.meta?.readOnly;
  const promptMode = automationConversation
    ? "assistant"
    : activeTabId
    ? normalizePromptMode(
        pendingPromptModesByTab[activeTabId] ?? promptModesByTab[activeTabId] ?? state.meta?.promptMode ?? activeTab?.promptMode,
        state.meta?.enhancedModeEnabled ?? activeTab?.enhancedModeEnabled,
        productCapabilities.promptModes,
      )
	: "assistant";
  const promptModeSwitching = activeTabId ? Boolean(promptModeSwitchingByTab[activeTabId]) : false;
  const promptModeSwitchFailed = activeTabId ? Boolean(promptModeSwitchFailedByTab[activeTabId]) : false;
  collaborationModeRef.current = collaborationMode;
  toolApprovalModeRef.current = toolApprovalMode;
  askWorkflowEnabledRef.current = askWorkflowEnabled;
  stepThinkingEnabledRef.current = stepThinkingEnabled;
  goalRef.current = goal;
  const displayedModelLabel = activeTabId
    ? pendingModelLabelsByTab[activeTabId] ?? state.meta?.label ?? t("status.connecting")
    : state.meta?.label ?? t("status.connecting");
  const displayedStatusModelLabel = activeTabId
    ? pendingModelLabelsByTab[activeTabId] ?? state.meta?.label
    : state.meta?.label;
  const displayedEffort = activeTabId && pendingEffortsByTab[activeTabId]
    ? { ...(state.effort ?? { supported: true, levels: [pendingEffortsByTab[activeTabId]], current: pendingEffortsByTab[activeTabId], default: pendingEffortsByTab[activeTabId] }), current: pendingEffortsByTab[activeTabId] }
    : state.effort;
  const controllerReady = state.meta?.ready === true;
  const setMode = useCallback(
    (next: Mode | ((prev: Mode) => Mode)) => {
      if (!activeTabId) return;
      setModesByTab((current) => {
        const prev = current[activeTabId] ?? "normal";
        const value = typeof next === "function" ? next(prev) : next;
        if (value === prev) return current;
        return { ...current, [activeTabId]: value };
      });
    },
    [activeTabId],
  );
  const setGoalDraftModeForTab = useCallback((tabId: string, enabled: boolean) => {
    setGoalDraftModesByTab((current) => {
      if (Boolean(current[tabId]) === enabled) return current;
      if (enabled) return { ...current, [tabId]: true };
      const next = { ...current };
      delete next[tabId];
      return next;
    });
  }, []);
  const topicbarEditing = Boolean(activeTab?.topicId && activeTab.topicId === renamingTopicId);
  useEffect(() => {
    const ids = new Set(tabMetas.map((tab) => tab.id));
    for (const id of Object.keys(yoloRestoreToolApprovalModesRef.current)) {
      if (!ids.has(id)) delete yoloRestoreToolApprovalModesRef.current[id];
    }
    setGoalDraftModesByTab((current) => {
      let changed = false;
      const next: Record<string, boolean> = {};
      for (const tab of tabMetas) {
        if (keepGoalDraftMode(Boolean(current[tab.id]), tab.goal)) {
          next[tab.id] = true;
        } else if (current[tab.id]) {
          changed = true;
        }
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setModesByTab((current) => {
      let changed = false;
      const next: Record<string, Mode> = {};
      for (const tab of tabMetas) {
        const mode = normalizeMode(tab.mode);
        next[tab.id] = mode;
        if (current[tab.id] !== mode) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setCollaborationModesByTab((current) => {
      let changed = false;
      const next: Record<string, CollaborationMode> = {};
      for (const tab of tabMetas) {
        const value = tabListCollaborationMode({
          goalDraftMode: keepGoalDraftMode(Boolean(goalDraftModesByTab[tab.id]), tab.goal),
          tabMode: tab.collaborationMode,
          tabGoal: tab.goal,
          legacyMode: normalizeMode(tab.mode),
        });
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setToolApprovalModesByTab((current) => {
      let changed = false;
      const next: Record<string, ToolApprovalMode> = {};
      for (const tab of tabMetas) {
        const value = normalizeToolApprovalMode(tab.toolApprovalMode, normalizeMode(tab.mode));
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setGoalsByTab((current) => {
      let changed = false;
      const next: Record<string, string> = {};
      for (const tab of tabMetas) {
        const value = tab.goal ?? "";
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
  }, [goalDraftModesByTab, tabMetas]);

  useEffect(() => {
    if (!renamingTopicId || activeTab?.topicId === renamingTopicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, [activeTab?.topicId, renamingTopicId]);

  useEffect(() => {
    if (!activeTabId || !state.meta) return;
    const nextGoal = state.meta.goalStatus === "running" ? state.meta.goal ?? "" : "";
    if (nextGoal) setGoalDraftModeForTab(activeTabId, false);
    setGoalsByTab((current) => (current[activeTabId] === nextGoal ? current : { ...current, [activeTabId]: nextGoal }));
    setCollaborationModesByTab((current) => {
      const nextMode = metaSyncedCollaborationMode({ nextGoal, goalDraftMode, legacyMode });
      return current[activeTabId] === nextMode ? current : { ...current, [activeTabId]: nextMode };
    });
    setAskWorkflowsByTab((current) => (current[activeTabId] === Boolean(state.meta?.askWorkflowEnabled) ? current : { ...current, [activeTabId]: Boolean(state.meta?.askWorkflowEnabled) }));
    setStepThinkingsByTab((current) => (current[activeTabId] === Boolean(state.meta?.stepThinkingEnabled) ? current : { ...current, [activeTabId]: Boolean(state.meta?.stepThinkingEnabled) }));
    setPromptModesByTab((current) => {
      const nextMode = normalizePromptMode(state.meta?.promptMode, state.meta?.enhancedModeEnabled, productCapabilities.promptModes);
      return current[activeTabId] === nextMode ? current : { ...current, [activeTabId]: nextMode };
    });
  }, [activeTabId, goalDraftMode, legacyMode, productCapabilities.promptModes, setGoalDraftModeForTab, state.meta]);

  const syncModeToController = useCallback((m: Mode) => setControllerMode(m), [setControllerMode]);

  useEffect(() => {
    void app.SetTrayLocale(locale).catch(() => {});
  }, [locale]);

  // applyMode is the single source of truth for the input mode: it updates the
  // local pill and pushes the matching gate state to the controller (plan = read
  // only; yolo = auto-approve approval-gated tools while user decisions still wait).
  // normal clears both.
  const applyMode = useCallback(
    (m: Mode) => {
      if (!activeTabId) return;
      const nextCollaborationMode: CollaborationMode = modeHasPlan(m) ? "plan" : "normal";
      const nextToolApprovalMode: ToolApprovalMode = modeHasAutoApproveTools(m) ? "yolo" : "ask";
      collaborationModeRef.current = nextCollaborationMode;
      toolApprovalModeRef.current = nextToolApprovalMode;
      goalRef.current = "";
      setGoalDraftModeForTab(activeTabId, false);
      setMode(m);
      setCollaborationModesByTab((current) => (current[activeTabId] === nextCollaborationMode ? current : { ...current, [activeTabId]: nextCollaborationMode }));
      setToolApprovalModesByTab((current) => (current[activeTabId] === nextToolApprovalMode ? current : { ...current, [activeTabId]: nextToolApprovalMode }));
      setGoalsByTab((current) => (current[activeTabId] ? { ...current, [activeTabId]: "" } : current));
      void syncModeToController(m);
    },
    [activeTabId, setGoalDraftModeForTab, setMode, syncModeToController],
  );
  const applyCollaborationMode = useCallback(
    (m: CollaborationMode) => {
      if (!activeTabId) return;
      if (m === "goal") {
        collaborationModeRef.current = "goal";
        goalRef.current = "";
        setGoalDraftModeForTab(activeTabId, true);
        setGoalsByTab((current) => (current[activeTabId] ? { ...current, [activeTabId]: "" } : current));
        setCollaborationModesByTab((current) => (current[activeTabId] === "goal" ? current : { ...current, [activeTabId]: "goal" }));
        setMode(modeFromAxes(false, toolApprovalMode === "yolo"));
        void (async () => {
          await clearControllerGoal();
          await setControllerCollaborationMode("normal");
        })();
        return;
      }
      collaborationModeRef.current = m;
      if (m === "normal" || m === "plan") goalRef.current = "";
      setGoalDraftModeForTab(activeTabId, false);
      setCollaborationModesByTab((current) => (current[activeTabId] === m ? current : { ...current, [activeTabId]: m }));
      if (m === "normal" || m === "plan") {
        setGoalsByTab((current) => (current[activeTabId] ? { ...current, [activeTabId]: "" } : current));
      }
      setMode(modeFromAxes(m === "plan", toolApprovalMode === "yolo"));
      void (async () => {
        if (m === "normal" || m === "plan") await clearControllerGoal();
        await setControllerCollaborationMode(m);
      })();
    },
    [activeTabId, clearControllerGoal, setControllerCollaborationMode, setGoalDraftModeForTab, setMode, toolApprovalMode],
  );
  const applyToolApprovalMode = useCallback(
    (m: ToolApprovalMode) => {
      if (!activeTabId) return;
      toolApprovalModeRef.current = m;
      if (m === "yolo") {
        if (toolApprovalMode !== "yolo") {
          yoloRestoreToolApprovalModesRef.current[activeTabId] = restorableToolApprovalMode(toolApprovalMode);
        }
      } else {
        yoloRestoreToolApprovalModesRef.current[activeTabId] = restorableToolApprovalMode(m);
      }
      setToolApprovalModesByTab((current) => (current[activeTabId] === m ? current : { ...current, [activeTabId]: m }));
      setMode(modeFromAxes(collaborationMode === "plan", m === "yolo"));
      void setControllerToolApprovalMode(m);
    },
    [activeTabId, collaborationMode, setControllerToolApprovalMode, setMode, toolApprovalMode],
  );
  const applyAskWorkflow = useCallback(
    (enabled: boolean) => {
      if (!activeTabId) return;
      askWorkflowEnabledRef.current = enabled;
      setAskWorkflowsByTab((current) => (current[activeTabId] === enabled ? current : { ...current, [activeTabId]: enabled }));
      void setControllerAskWorkflow(enabled);
    },
    [activeTabId, setControllerAskWorkflow],
  );
  const applyStepThinking = useCallback(
    (enabled: boolean) => {
      if (!activeTabId) return;
      stepThinkingEnabledRef.current = enabled;
      setStepThinkingsByTab((current) => (current[activeTabId] === enabled ? current : { ...current, [activeTabId]: enabled }));
      void setControllerStepThinking(enabled);
    },
    [activeTabId, setControllerStepThinking],
  );
  const applyPromptMode = useCallback(
    async (mode: PromptMode) => {
      if (!activeTabId || mode === promptMode || !productCapabilities.promptModes.includes(mode)) return;
	  const hasConversationHistory = state.items.some(
		(item) => (item.kind === "user" || item.kind === "assistant") && item.text.trim().length > 0,
	  ) || Boolean(state.live?.text.trim());
	  if (hasConversationHistory) {
		const confirmed = await app.ConfirmAction({
		  title: t("composer.promptMode.switchTitle"),
		  message: t("composer.promptMode.switchMessage"),
		  detail: t("composer.promptMode.switchDetail"),
		  confirmLabel: t("composer.promptMode.switchConfirm"),
		  cancelLabel: t("common.cancel"),
		  destructive: false,
		});
		if (!confirmed) return;
	  }
      latestPromptModeSwitchRef.current[activeTabId] = mode;
      setPromptModeSwitchFailedByTab((current) => {
        if (!current[activeTabId]) return current;
        const next = { ...current };
        delete next[activeTabId];
        return next;
      });
      if (!pendingPromptModeOriginRef.current[activeTabId]) {
        pendingPromptModeOriginRef.current[activeTabId] = promptMode;
      }
      setPendingPromptModesByTab((current) => (current[activeTabId] === mode ? current : { ...current, [activeTabId]: mode }));
      setPromptModesByTab((current) => (current[activeTabId] === mode ? current : { ...current, [activeTabId]: mode }));
      if (runningRef.current) {
        return;
      }
      if (promptModeSwitchingRef.current[activeTabId]) return;
      promptModeSwitchingRef.current[activeTabId] = true;
      setPromptModeSwitchingByTab((current) => (current[activeTabId] ? current : { ...current, [activeTabId]: true }));
      const releaseSwitchLock = (settledMode: PromptMode) => {
        delete promptModeSwitchingRef.current[activeTabId];
        setPendingPromptModesByTab((current) => {
          if (current[activeTabId] !== settledMode) return current;
          const next = { ...current };
          delete next[activeTabId];
          return next;
        });
        setPromptModeSwitchingByTab((current) => {
          if (!current[activeTabId]) return current;
          const next = { ...current };
          delete next[activeTabId];
          return next;
        });
      };
      let settledMode = mode;
      try {
        while (true) {
          const target = latestPromptModeSwitchRef.current[activeTabId] ?? settledMode;
          settledMode = target;
		  await app.RequestConversationModeForTab(activeTabId, target);
          if (latestPromptModeSwitchRef.current[activeTabId] === target) break;
        }
		await refreshMeta();
        setPromptModeSwitchFailedByTab((current) => {
          if (!current[activeTabId]) return current;
          const next = { ...current };
          delete next[activeTabId];
          return next;
        });
        delete latestPromptModeSwitchRef.current[activeTabId];
        delete pendingPromptModeOriginRef.current[activeTabId];
      } catch (err) {
        delete latestPromptModeSwitchRef.current[activeTabId];
        const origin = pendingPromptModeOriginRef.current[activeTabId] ?? promptMode;
        delete pendingPromptModeOriginRef.current[activeTabId];
        setPromptModesByTab((current) => (current[activeTabId] === origin ? current : { ...current, [activeTabId]: origin }));
        setPendingPromptModesByTab((current) => {
          const next = { ...current };
          delete next[activeTabId];
          return next;
        });
        setPromptModeSwitchFailedByTab((current) => ({ ...current, [activeTabId]: true }));
        showToast(err instanceof Error ? err.message : String(err), "error");
      } finally {
        releaseSwitchLock(settledMode);
      }
    },
	[activeTabId, productCapabilities.promptModes, promptMode, refreshMeta, showToast, state.items, state.live?.text, t],
  );
  const toggleYoloApprovalMode = useCallback(() => {
    if (!activeTabId) return;
    const next = toggleYoloToolApprovalMode(
      toolApprovalMode,
      yoloRestoreToolApprovalModesRef.current[activeTabId],
    );
    if (next.restore) {
      yoloRestoreToolApprovalModesRef.current[activeTabId] = next.restore;
    }
    applyToolApprovalMode(next.mode);
  }, [activeTabId, applyToolApprovalMode, toolApprovalMode]);
  const applyGoal = useCallback(
    async (nextGoal: string) => {
      if (!activeTabId) return;
      const trimmed = nextGoal.trim();
      goalRef.current = trimmed;
      collaborationModeRef.current = trimmed ? "goal" : "normal";
      setGoalDraftModeForTab(activeTabId, false);
      setGoalsByTab((current) => (current[activeTabId] === trimmed ? current : { ...current, [activeTabId]: trimmed }));
      setCollaborationModesByTab((current) => {
        const nextMode = trimmed ? "goal" : "normal";
        return current[activeTabId] === nextMode ? current : { ...current, [activeTabId]: nextMode };
      });
      setMode(modeFromAxes(false, toolApprovalMode === "yolo"));
      if (trimmed) await setControllerGoal(trimmed);
      else await clearControllerGoal();
    },
    [activeTabId, clearControllerGoal, setControllerGoal, setGoalDraftModeForTab, setMode, toolApprovalMode],
  );
  // Shift+Tab toggles only the collaboration axis; Ctrl/Cmd+Y toggles YOLO on the
  // tool-permission axis while preserving the Ask/Auto base mode.
  const cycleMode = useCallback(() => {
    applyCollaborationMode(collaborationMode === "plan" ? "normal" : "plan");
  }, [applyCollaborationMode, collaborationMode]);

  // Switching models rebuilds the controller, which starts in normal mode — so
  // re-apply the current mode, or the pill would say plan/YOLO while the fresh
  // controller silently uses normal gating.
  const switchModel = useCallback(
    async (name: string, displayLabel?: string) => {
      if (!activeTabId) return;
      const pendingLabel = modelDisplayLabel(name, displayLabel);
      latestModelSwitchRef.current[activeTabId] = name;
      setPendingModelLabelsByTab((current) => (current[activeTabId] === pendingLabel ? current : { ...current, [activeTabId]: pendingLabel }));
      if (runningRef.current) return;
      const applyRuntimePrefs = async () => {
        const nextCollaborationMode = collaborationModeRef.current;
        const nextGoal = goalRef.current;
        await setControllerCollaborationMode(controllerCollaborationMode({ collaborationMode: nextCollaborationMode, goal: nextGoal }));
        await setControllerToolApprovalMode(toolApprovalModeRef.current);
        await setControllerAskWorkflow(askWorkflowEnabledRef.current);
        await setControllerStepThinking(stepThinkingEnabledRef.current);
        if (nextGoal.trim()) await setControllerGoal(nextGoal);
      };
      const task = (async () => {
        try {
          await setModel(name);
          await applyRuntimePrefs();
          await refreshMeta();
        } finally {
          if (latestModelSwitchRef.current[activeTabId] === name) {
            delete latestModelSwitchRef.current[activeTabId];
            delete pendingModelSwitchRef.current[activeTabId];
            setPendingModelLabelsByTab((current) => {
              if (current[activeTabId] !== pendingLabel) return current;
              const next = { ...current };
              delete next[activeTabId];
              return next;
            });
          }
        }
      })();
      pendingModelSwitchRef.current[activeTabId] = task;
      await task;
    },
    [activeTabId, askWorkflowEnabled, collaborationMode, goal, refreshMeta, setControllerAskWorkflow, setControllerCollaborationMode, setControllerGoal, setControllerStepThinking, setControllerToolApprovalMode, setModel, stepThinkingEnabled, toolApprovalMode],
  );

  const switchEffort = useCallback(
    async (level: string) => {
      if (!activeTabId) return;
      latestEffortSwitchRef.current[activeTabId] = level;
      setPendingEffortsByTab((current) => (current[activeTabId] === level ? current : { ...current, [activeTabId]: level }));
      if (runningRef.current) return;
      const task = (async () => {
        try {
          await setEffort(level);
          await refreshMeta();
        } finally {
          if (latestEffortSwitchRef.current[activeTabId] === level) {
            delete latestEffortSwitchRef.current[activeTabId];
            delete pendingEffortSwitchRef.current[activeTabId];
            setPendingEffortsByTab((current) => {
              if (current[activeTabId] !== level) return current;
              const next = { ...current };
              delete next[activeTabId];
              return next;
            });
          }
        }
      })();
      pendingEffortSwitchRef.current[activeTabId] = task;
      await task;
    },
    [activeTabId, refreshMeta, setEffort],
  );

  const applyPendingRuntimePrefs = useCallback(async (tabId: string) => {
    const pendingModel = latestModelSwitchRef.current[tabId];
    if (pendingModel && !pendingModelSwitchRef.current[tabId]) {
      latestModelSwitchRef.current[tabId] = pendingModel;
      const task = (async () => {
        try {
          await app.SetModelForTab(tabId, pendingModel);
          await app.SetCollaborationModeForTab(tabId, controllerCollaborationMode({ collaborationMode: collaborationModeRef.current, goal: goalRef.current }));
          await app.SetToolApprovalModeForTab(tabId, toolApprovalModeRef.current);
          await app.SetAskWorkflowForTab(tabId, askWorkflowEnabledRef.current);
          await app.SetStepThinkingForTab(tabId, stepThinkingEnabledRef.current);
          if (goalRef.current.trim()) await app.SetGoalForTab(tabId, goalRef.current);
          await refreshMeta();
        } finally {
          if (latestModelSwitchRef.current[tabId] === pendingModel) {
            delete latestModelSwitchRef.current[tabId];
            delete pendingModelSwitchRef.current[tabId];
            setPendingModelLabelsByTab((current) => {
              if (current[tabId] !== pendingModel) return current;
              const next = { ...current };
              delete next[tabId];
              return next;
            });
          }
        }
      })();
      pendingModelSwitchRef.current[tabId] = task;
    }
    await pendingModelSwitchRef.current[tabId];
    const pendingEffort = latestEffortSwitchRef.current[tabId] ?? pendingEffortsByTab[tabId];
    if (pendingEffort && !pendingEffortSwitchRef.current[tabId]) {
      latestEffortSwitchRef.current[tabId] = pendingEffort;
      const task = (async () => {
        try {
          await app.SetEffortForTab(tabId, pendingEffort);
          await refreshMeta();
        } finally {
          if (latestEffortSwitchRef.current[tabId] === pendingEffort) {
            delete latestEffortSwitchRef.current[tabId];
            delete pendingEffortSwitchRef.current[tabId];
            setPendingEffortsByTab((current) => {
              if (current[tabId] !== pendingEffort) return current;
              const next = { ...current };
              delete next[tabId];
              return next;
            });
          }
        }
      })();
      pendingEffortSwitchRef.current[tabId] = task;
    }
    await pendingEffortSwitchRef.current[tabId];
    const hasPendingPromptMode = Object.prototype.hasOwnProperty.call(pendingPromptModesByTab, tabId);
    if (hasPendingPromptMode && !pendingPromptModeSwitchRef.current[tabId]) {
      const nextPromptMode = pendingPromptModesByTab[tabId];
      const task = (async () => {
        try {
		  await app.RequestConversationModeForTab(tabId, nextPromptMode);
          await refreshMeta();
          setPromptModeSwitchFailedByTab((current) => {
            if (!current[tabId]) return current;
            const next = { ...current };
            delete next[tabId];
            return next;
          });
          delete pendingPromptModeOriginRef.current[tabId];
        } catch (err) {
          const origin = pendingPromptModeOriginRef.current[tabId];
          delete pendingPromptModeOriginRef.current[tabId];
          if (origin) {
            setPromptModesByTab((current) => (current[tabId] === origin ? current : { ...current, [tabId]: origin }));
          }
          setPromptModeSwitchFailedByTab((current) => ({ ...current, [tabId]: true }));
          showToast(err instanceof Error ? err.message : String(err), "error");
          throw err;
        } finally {
          delete pendingPromptModeSwitchRef.current[tabId];
          if (latestPromptModeSwitchRef.current[tabId] === nextPromptMode) delete latestPromptModeSwitchRef.current[tabId];
          setPendingPromptModesByTab((current) => {
            if (current[tabId] !== nextPromptMode) return current;
            const next = { ...current };
            delete next[tabId];
            return next;
          });
        }
      })();
      pendingPromptModeSwitchRef.current[tabId] = task;
    }
    await pendingPromptModeSwitchRef.current[tabId];
  }, [pendingEffortsByTab, pendingPromptModesByTab, refreshMeta, showToast]);

  useEffect(() => {
    if (!activeTabId || state.running) return;
    if (!Object.prototype.hasOwnProperty.call(pendingPromptModesByTab, activeTabId)) return;
    if (pendingPromptModeSwitchRef.current[activeTabId]) return;
    void applyPendingRuntimePrefs(activeTabId);
  }, [activeTabId, applyPendingRuntimePrefs, pendingPromptModesByTab, state.running]);

  const startGoal = useCallback(
    async (nextGoal: string) => {
      const trimmed = nextGoal.trim();
      if (!trimmed) return;
      if (activeTabId) {
        await applyPendingRuntimePrefs(activeTabId);
      }
      await applyGoal(trimmed);
      try {
        await send(trimmed, `/goal ${trimmed}`);
      } catch {
        // send already records the failed optimistic turn; this callback is
        // also used by the goal-mode toggle, which cannot await a Promise.
      }
    },
    [activeTabId, applyGoal, applyPendingRuntimePrefs, send],
  );

  const submitPromptToAgent = useCallback(
    async (displayText: string, submitText = displayText) => {
      const trimmed = displayText.trim();
      if (!trimmed) return;
      if (activeTabId) {
        await applyPendingRuntimePrefs(activeTabId);
      }
      const nextCollaborationMode = collaborationModeRef.current;
      const nextGoal = goalRef.current;
      await setControllerCollaborationMode(controllerCollaborationMode({ collaborationMode: nextCollaborationMode, goal: nextGoal }));
      await setControllerToolApprovalMode(toolApprovalModeRef.current);
      await setControllerAskWorkflow(askWorkflowEnabledRef.current);
      await setControllerStepThinking(stepThinkingEnabledRef.current);
      if (nextGoal.trim()) await setControllerGoal(nextGoal);
      await send(trimmed, submitText.trim());
    },
    [activeTabId, applyPendingRuntimePrefs, askWorkflowEnabled, collaborationMode, goal, send, setControllerAskWorkflow, setControllerCollaborationMode, setControllerGoal, setControllerStepThinking, setControllerToolApprovalMode, stepThinkingEnabled, toolApprovalMode],
  );

  const queuePrompt = useCallback((displayText: string, submitText = displayText) => {
    if (!activeTabId) return;
    const display = displayText.trim();
    const submit = submitText.trim();
    if (!display || !submit) return;
    const queued: QueuedPrompt = {
      id: `queued-${Date.now()}-${nextQueuedPromptIdRef.current++}`,
      displayText: display,
      submitText: submit,
    };
    setQueuedPromptsByTab((current) => ({
      ...current,
      [activeTabId]: [...(current[activeTabId] ?? []), queued],
    }));
  }, [activeTabId]);

  const removeQueuedPrompt = useCallback((id: string) => {
    if (!activeTabId) return;
    setQueuedPromptsByTab((current) => {
      const nextQueue = (current[activeTabId] ?? []).filter((item) => item.id !== id);
      if (nextQueue.length === (current[activeTabId] ?? []).length) return current;
      const next = { ...current };
      if (nextQueue.length > 0) next[activeTabId] = nextQueue;
      else delete next[activeTabId];
      return next;
    });
  }, [activeTabId]);

  const guideQueuedPrompt = useCallback((prompt: QueuedPrompt) => {
    steer(prompt.submitText);
    removeQueuedPrompt(prompt.id);
  }, [removeQueuedPrompt, steer]);

  const handleGuide = useCallback((displayText: string, submitText = displayText) => {
    const text = (submitText || displayText).trim();
    if (!text) return;
    steer(text);
  }, [steer]);

  // Startup and workspace/model rebuilds create a fresh controller in normal
  // mode. Re-apply the UI mode once the controller is ready, including the case
  // where the user picked YOLO while boot was still loading and the legacy
  // SetBypass binding was a harmless no-op.
  useEffect(() => {
    if (!controllerReady) return;
    void setControllerCollaborationMode(controllerCollaborationMode({ collaborationMode, goal }));
    void setControllerToolApprovalMode(toolApprovalMode);
    void setControllerAskWorkflow(askWorkflowEnabled);
    void setControllerStepThinking(stepThinkingEnabled);
    if (goal.trim()) void setControllerGoal(goal);
  }, [askWorkflowEnabled, collaborationMode, controllerReady, goal, setControllerAskWorkflow, setControllerCollaborationMode, setControllerGoal, setControllerStepThinking, setControllerToolApprovalMode, stepThinkingEnabled, toolApprovalMode]);

  // The live task list pinned above the composer comes from the most recent
  // successful top-level todo_write result; failed or still-running attempts do
  // not advance the canonical panel state. It stays visible through the final
  // all-completed update, and can be dismissed by the user (the ✕). A dismissal
  // is keyed to that list's id, so a fresh accepted todo_write brings the panel
  // back.
  const todoEntry = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const it = state.items[i];
      if (it.kind === "tool" && it.name === "todo_write" && !it.parentId && it.status === "done" && !it.error) {
        return { item: it, index: i };
      }
    }
    return null;
  }, [state.items]);
  const todoItem = todoEntry?.item ?? null;
  const todos = useMemo(() => (todoItem ? parseTodos(todoItem.args) : []), [todoItem]);
  const [dismissedTodo, setDismissedTodo] = useState<string | null>(null);
  const showTodos = shouldShowTodoPanel(todoItem?.id, dismissedTodo, todos);

  const sessionTitle = topicTitle(activeTab);
  const sessionHasContent = state.items.length > 0 || Boolean(state.live?.text || state.live?.reasoning);
  const getSessionMarkdown = useCallback(
    () => sessionItemsToMarkdown(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );
  const getSessionJson = useCallback(
    () => sessionItemsToJson(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );

  useEffect(() => {
    if (!topicExportOpen) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (!target?.closest(".topicbar__export")) setTopicExportOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [topicExportOpen]);
  useEffect(() => {
    if (!topicOverflowOpen) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (!target?.closest(".topicbar__overflow")) setTopicOverflowOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [topicOverflowOpen]);

  const exportSession = useCallback(
    async (format: "markdown" | "json" | "pdf" | "image") => {
      const base = safeFilename(sessionTitle);
      setTopicExportOpen(false);
      try {
        if (format === "json") {
          const path = await app.PickExportFile(`${base}.json`, "application/json");
          if (path) await app.SaveExportFile(path, getSessionJson(), false);
        } else if (format === "pdf") {
          const path = await app.PickExportFile(`${base}.pdf`, "application/pdf");
          if (!path) return;
          const blob = await renderSessionPdfBlob(getSessionMarkdown(), sessionTitle);
          await app.SaveExportFile(path, await blobToBase64(blob), true);
        } else if (format === "image") {
          const path = await app.PickExportFile(`${base}.png`, "image/png");
          if (!path) return;
          const blob = await renderSessionImageBlob(getSessionMarkdown());
          await app.SaveExportFile(path, await blobToBase64(blob), true);
        } else {
          const path = await app.PickExportFile(`${base}.md`, "text/markdown");
          if (path) await app.SaveExportFile(path, getSessionMarkdown(), false);
        }
      } catch (err) {
        console.error("Failed to export session", err);
      }
    },
    [getSessionJson, getSessionMarkdown, sessionTitle],
  );

  useEffect(() => {
    if (!pendingPlanRevision || state.running) return;
    const text = pendingPlanRevision;
    setPendingPlanRevision(null);
    void send(text).catch(() => {});
  }, [pendingPlanRevision, send, state.running]);

  useEffect(() => {
    setClearContextPending(false);
  }, [activeTabId]);

  const cancelClearContext = useCallback(() => {
    setClearContextPending(false);
  }, []);

  const confirmClearContext = useCallback(async () => {
    setClearContextPending(false);
    try {
      await clearSession();
      notice(t("clearContext.done"));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      notice(msg || t("clearContext.failed"), "warn");
    }
  }, [clearSession, notice, t]);

  // Keep runningRef in sync so handleSend sees the latest running value
  // even inside a stale closure.
  useEffect(() => {
    runningRef.current = state.running;
  }, [state.running]);

  // handleSend intercepts slash commands that need a desktop-native action before
  // they reach the backend: "/model <ref>" rebuilds on that model, "/memory"
  // opens Settings, and "/clear" shows an in-app confirmation card. Everything else — skills (/init, …),
  // custom commands, bare /model and the other read-only management verbs
  // (/skill, /hooks, /mcp) — goes straight to Submit, which the controller
  // resolves (a turn, or a listing Notice).
  const handleSend = useCallback(
    async (displayText: string, submitText = displayText) => {
      const trimmed = displayText.trim();
      // "!<cmd>" runs a shell command directly, bypassing the model.
      if (trimmed.startsWith("!")) {
        const cmd = trimmed.slice(1).trim();
        if (!cmd) {
          notice("usage: !<command>  (e.g. !ls -la)");
          return;
        }
        runShell(cmd);
        return;
      }
      const model = /^\/model\s+(\S+)$/.exec(trimmed);
      if (model) {
        void switchModel(model[1]);
        return;
      }
      if (trimmed === "/memory") {
        closeTransientOverlays();
        setSettingsTarget("memory");
        return;
      }
      if (trimmed === "/clear") {
        setClearContextPending(true);
        return;
      }
      const goalCommand = /^\/goal(?:\s+(.*))?$/.exec(trimmed);
      if (goalCommand) {
        const arg = (goalCommand[1] ?? "").trim();
        if (arg && !["status", "clear", "off", "stop", "done"].includes(arg.toLowerCase())) {
          await applyGoal(arg);
        } else if (["clear", "off", "stop", "done"].includes(arg.toLowerCase())) {
          await applyGoal("");
        }
        if (activeTabId) {
          await applyPendingRuntimePrefs(activeTabId);
        }
        await send(trimmed, submitText.trim());
        return;
      }
      if (collaborationModeRef.current === "goal" && !goalRef.current.trim()) {
        if (activeTabId) {
          await applyPendingRuntimePrefs(activeTabId);
        }
        await applyGoal(trimmed);
        await send(trimmed, `/goal ${submitText.trim()}`);
        return;
      }
      if (runningRef.current || Boolean(activeTabId && promptModeSwitchingRef.current[activeTabId])) {
        queuePrompt(trimmed, submitText.trim());
        return;
      }
      await submitPromptToAgent(trimmed, submitText.trim());
    },
    [activeTabId, applyGoal, applyPendingRuntimePrefs, closeTransientOverlays, collaborationMode, goal, queuePrompt, runShell, notice, submitPromptToAgent, switchModel, t],
  );

  useEffect(() => {
    if (state.running) queuedPromptDispatchingRef.current = false;
  }, [state.running]);

  useEffect(() => {
    if (!activeTabId || state.running || promptModeSwitching || promptModeSwitchFailed || state.approval || state.ask || state.messageAction) return;
    if (queuedPromptDispatchingRef.current) return;
    const nextPrompt = queuedPromptsByTab[activeTabId]?.[0];
    if (!nextPrompt) return;
    queuedPromptDispatchingRef.current = true;
    void submitPromptToAgent(nextPrompt.displayText, nextPrompt.submitText).then(() => {
      setQueuedPromptsByTab((current) => {
        const queue = current[activeTabId] ?? [];
        if (queue[0]?.id !== nextPrompt.id) return current;
        const rest = queue.slice(1);
        const next = { ...current };
        if (rest.length > 0) next[activeTabId] = rest;
        else delete next[activeTabId];
        return next;
      });
      queuedPromptDispatchingRef.current = false;
    }).catch(() => {
      queuedPromptDispatchingRef.current = false;
    });
  }, [activeTabId, promptModeSwitchFailed, promptModeSwitching, queuedPromptsByTab, state.approval, state.ask, state.messageAction, state.running, submitPromptToAgent]);

  const refreshTabMetas = useCallback(async (): Promise<TabMeta[]> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    const signature = JSON.stringify(tabs.map((tab) => [
      tab.id,
      tab.active,
      tab.running,
      tab.ready,
      tab.startupErr,
      tab.label,
      tab.topicTitle,
      tab.mode,
      tab.collaborationMode,
      tab.toolApprovalMode,
      tab.promptMode,
      tab.workspaceRoot,
      tab.topicId,
    ]));
    if (signature !== tabMetasSignatureRef.current) {
      tabMetasSignatureRef.current = signature;
      setTabMetas(tabs);
    }
    return tabs;
  }, []);

  const openBlankSession = useCallback(async (scope: string, workspaceRoot: string) => {
    await ensureBlankTab(scope, scope === "project" ? workspaceRoot : "");
    setProjectRevision((value) => value + 1);
    await refreshTabMetas();
  }, [ensureBlankTab, refreshTabMetas]);

  useEffect(() => {
    void refreshTabMetas();
    const id = window.setInterval(() => void refreshTabMetas(), 12000);
    return () => window.clearInterval(id);
  }, [refreshTabMetas]);

  useEffect(() => {
    let timer: number | null = null;
    const off = onProjectTreeChanged(() => {
      if (timer !== null) window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        timer = null;
        setProjectRevision((value) => value + 1);
        void refreshTabMetas();
      }, 350);
    });
    return () => {
      off();
      if (timer !== null) window.clearTimeout(timer);
    };
  }, [refreshTabMetas]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const needs = await app.NeedsOnboarding();
        if (!cancelled) setNeedsOnboarding(needs);
      } catch {
        // Bridge unavailable (browser dev seam) — skip the gate; a real key
        // failure still surfaces via the topbar startupError banner.
        if (!cancelled) setNeedsOnboarding(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const el = footerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let frame = 0;
    const update = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        const next = Math.round(el.getBoundingClientRect().height);
        if (Math.abs(footerHeightRef.current - next) < 2) return;
        footerHeightRef.current = next;
        setFooterHeight(next);
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, []);

  const toggleSidebar = useCallback(() => {
    closeTransientOverlays();
    pulseSidebarToggle();
    anchorAppScrollToChat();
    const nextCollapsed = !sidebarCollapsed;
    setSidebarCollapsed(nextCollapsed);
    saveSidebarCollapsed(nextCollapsed);
  }, [anchorAppScrollToChat, closeTransientOverlays, pulseSidebarToggle, sidebarCollapsed]);

  const setExpandedSidebarWidth = useCallback((width: number) => {
    closeTransientOverlays();
    const next = clampSidebarWidth(width);
    setSidebarWidth(next);
    saveSidebarWidth(next);
  }, [closeTransientOverlays]);

  const startSidebarResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      event.preventDefault();
      closeTransientOverlays();
      setSidebarResizing(true);
      let nextWidth = sidebarWidth;
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampSidebarWidth(moveEvent.clientX);
        setSidebarWidth(nextWidth);
      };
      const onDone = () => {
        setSidebarWidth(nextWidth);
        saveSidebarWidth(nextWidth);
        setSidebarResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [closeTransientOverlays, sidebarCollapsed, sidebarWidth],
  );

  const resizeSidebarWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setExpandedSidebarWidth(sidebarWidth + (event.key === "ArrowRight" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MAX_WIDTH);
      }
    },
    [setExpandedSidebarWidth, sidebarCollapsed, sidebarWidth],
  );

  const setSavedWorkspacePanelWidth = useCallback(
    (width: number) => {
      closeTransientOverlays();
      if (rightDockDetailActive) {
        const next = clampRightDockPreviewWidth(width);
        setRightDockPreviewWidth(next);
        saveRightDockPreviewWidth(next);
        return;
      }
      const next = clampRightDockTreeWidth(width);
      setRightDockTreeWidth(next);
      saveRightDockTreeWidth(next);
    },
    [closeTransientOverlays, rightDockDetailActive],
  );

  const ensureWorkspacePanelWidth = useCallback(
    (width: number) => {
      closeTransientOverlays();
      if (rightDockMode === "context") return;
      const next = clampRightDockPreviewWidth(width);
      setRightDockPreviewWidth(next);
      saveRightDockPreviewWidth(next);
    },
    [closeTransientOverlays, rightDockMode],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!workspacePanelOpen) return;
      event.preventDefault();
      closeTransientOverlays();
      setWorkspacePanelResizing(true);
      const startX = event.clientX;
      const startDockWidth = workspacePanelRenderWidth;
      let nextDockWidth = startDockWidth;
      const onMove = (moveEvent: PointerEvent) => {
        const delta = moveEvent.clientX - startX;
        nextDockWidth = startDockWidth - delta;
        if (rightDockDetailActive) {
          setRightDockPreviewWidth(clampRightDockPreviewWidth(nextDockWidth));
        } else {
          setRightDockTreeWidth(clampRightDockTreeWidth(nextDockWidth));
        }
      };
      const onDone = () => {
        setSavedWorkspacePanelWidth(nextDockWidth);
        setWorkspacePanelResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [closeTransientOverlays, rightDockDetailActive, setSavedWorkspacePanelWidth, workspacePanelOpen, workspacePanelRenderWidth],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePanelRenderWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH);
      }
    },
    [rightDockDetailActive, setSavedWorkspacePanelWidth, workspacePanelRenderWidth],
  );

  const openWorkspacePanel = useCallback(
    (mode: RightDockMode = rightDockMode) => {
      closeTransientOverlays();
      if (mode === "context" || mode !== rightDockMode) {
        setWorkspacePreviewActive(false);
      }
      setRightDockMode(mode);
      let nextMaximized = workspacePanelMaximized;
      if (mode === "context") {
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      } else {
        // Keep file/change views docked; the rendered dock width is clamped to
        // the viewport so opening it reflows instead of forcing maximize.
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      }
      if (workspacePanelOpen && workspacePanelMaximized === nextMaximized) {
        return;
      }
      setWorkspacePanelOpen(true);
    },
    [closeTransientOverlays, rightDockMode, workspacePanelMaximized, workspacePanelOpen],
  );

  const closeWorkspacePanel = useCallback(() => {
    closeTransientOverlays();
    if (!workspacePanelOpen) {
      return;
    }
    setWorkspacePanelMaximized(false);
    setWorkspacePanelOpen(false);
  }, [closeTransientOverlays, workspacePanelOpen]);

  const toggleWorkspacePanel = useCallback(() => {
    pulseWorkspaceToggle();
    if (workspacePanelRenderable) {
      closeWorkspacePanel();
      return;
    }
    openWorkspacePanel("context");
  }, [closeWorkspacePanel, openWorkspacePanel, pulseWorkspaceToggle, workspacePanelRenderable]);

  const openRightDockMode = useCallback(
    (mode: RightDockMode) => {
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      openWorkspacePanel(mode);
    },
    [openWorkspacePanel],
  );

  const openRightDockFile = useCallback(
    (path: string) => {
      const nextPath = path.trim();
      if (!nextPath) return;
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceRevealRequest((current) => ({ id: (current?.id ?? 0) + 1, path: nextPath }));
      openWorkspacePanel("files");
    },
    [openWorkspacePanel],
  );

  const openRightDockFileList = useCallback(
    (paths: string[]) => {
      const normalized = Array.from(new Set(paths.map((path) => path.trim()).filter(Boolean)));
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceFileListRequest((current) =>
        normalized.length > 0
          ? { id: (current?.id ?? 0) + 1, paths: normalized }
          : null,
      );
      openWorkspacePanel("files");
    },
    [openWorkspacePanel],
  );

  const openRightDockChangeFile = useCallback(
    (path: string) => {
      const nextPath = path.trim();
      if (!nextPath) return;
      setWorkspaceRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceChangeRevealRequest((current) => ({ id: (current?.id ?? 0) + 1, path: nextPath }));
      openWorkspacePanel("changed");
    },
    [openWorkspacePanel],
  );

  const openRightDockChangeList = useCallback(
    (changes: WorkspaceChangeListEntry[]) => {
      const seen = new Set<string>();
      const normalized = changes
        .map((change) => ({ ...change, path: change.path.trim() }))
        .filter((change) => {
          if (!change.path || seen.has(change.path)) return false;
          seen.add(change.path);
          return true;
      });
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest((current) =>
        normalized.length > 0
          ? { id: (current?.id ?? 0) + 1, changes: normalized }
          : null,
      );
      openWorkspacePanel("changed");
    },
    [openWorkspacePanel],
  );

  const handleWorkspacePreviewModeChange = useCallback(
    (active: boolean) => {
      if (workspacePreviewActive === active) return;
      closeTransientOverlays();
      setWorkspacePreviewActive(active);
    },
    [closeTransientOverlays, workspacePreviewActive],
  );

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${sidebarWidth}px`,
        "--chat-min-width": `${chatReservedWidth}px`,
        "--workspace-width": `${workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
      }) as CSSProperties,
    [chatReservedWidth, sidebarWidth, workspacePanelRenderWidth],
  );

  const setWorkspacePanel = useCallback((open: boolean) => {
    if (open) {
      openWorkspacePanel();
    } else {
      closeWorkspacePanel();
    }
  }, [closeWorkspacePanel, openWorkspacePanel]);

  const addWorkspaceTextToComposer = useCallback((text: string) => {
    setComposerInsertRequest({ id: Date.now(), text });
  }, []);

  const replaceComposerText = useCallback((text: string) => {
    setComposerInsertRequest({ id: Date.now(), text, mode: "replace" });
  }, []);

  const handleNewTab = useCallback(() => {
    closeTransientOverlays();
    setNewSessionChooserOpen(true);
  }, [closeTransientOverlays]);

  const handleNewSessionChoice = useCallback(async (scope: "global" | "project" | "automation", workspaceRoot: string) => {
    setNewSessionChooserOpen(false);
    await openBlankSession(scope, scope === "project" ? workspaceRoot : "");
  }, [openBlankSession]);

  const handleMessageAction = useCallback(async (turn: number, scope: string) => {
    await rewind(turn, scope);
    if (scope === "fork") {
      await refreshTabMetas();
      setProjectRevision((value) => value + 1);
      return;
    }
    if (scope === "code" || scope === "both") {
      setDockRefreshKey((value) => value + 1);
      setProjectRevision((value) => value + 1);
    }
  }, [refreshTabMetas, rewind]);

  const handleOpenTopic = useCallback(async (scope: string, workspaceRoot: string, topicId: string) => {
    closeTransientOverlays();
    const existing = tabMetas.find((tab) =>
      tab.topicId === topicId &&
      tab.scope === scope &&
      (scope === "global" || tab.workspaceRoot === workspaceRoot),
    );
    if (existing) {
      await switchTab(existing.id);
      void refreshTabMetas();
      return;
    }
    setPendingTopic({ scope, workspaceRoot, topicId });
    try {
      if (scope === "global") await openGlobalTab(topicId);
      else if (scope === "automation") await openAutomationTab(topicId);
      else await openProjectTab(workspaceRoot, topicId);
      await refreshTabMetas();
    } finally {
      setPendingTopic((current) => current?.topicId === topicId ? null : current);
    }
  }, [closeTransientOverlays, openAutomationTab, openGlobalTab, openProjectTab, refreshTabMetas, switchTab, tabMetas]);

  // History drawer: project menus can open a scoped saved-session list. Idle row
  // clicks resume; running row clicks only preview through PreviewSession.
  const openProjectHistory = useCallback(async (scope: "global" | "project", workspaceRoot: string) => {
    closeTransientOverlays();
    const filter = { scope, workspaceRoot };
    setHistView({ kind: "history", source: "scope", filter, sessions: sessionsForScope(await listSessions(), filter) });
  }, [closeTransientOverlays, listSessions]);
  const openAllHistory = useCallback(async () => {
    closeTransientOverlays();
    setHistView({ kind: "history", source: "all", sessions: await listSessions() });
  }, [closeTransientOverlays, listSessions]);
  const openTrash = useCallback(async () => {
    closeTransientOverlays();
    setHistView({ kind: "trash", sessions: await listTrashedSessions() });
  }, [closeTransientOverlays, listTrashedSessions]);
  const closeHistory = useCallback(() => {
    closeTransientOverlays();
    setHistView(null);
  }, [closeTransientOverlays]);

  const onResumeSession = useCallback(
    async (session: SessionMeta) => {
      if (state.running) return;
      const scope = session.scope || (session.workspaceRoot ? "project" : "global");
      try {
        let targetTab: TabMeta;
        if (scope === "project" && session.workspaceRoot && session.topicId) {
          targetTab = await openProjectTab(session.workspaceRoot, session.topicId);
        } else if (scope === "global" && session.topicId) {
          targetTab = await openGlobalTab(session.topicId);
        } else {
          throw new Error(scope === "global" && !session.topicId
            ? t("history.failedOpenSession")
            : (session.topicId ? "Missing workspaceRoot" : t("history.failedOpenSession")));
        }
        setHistView(null);
        await resumeSession(session.path, targetTab.id);
        await refreshTabMetas();
      } catch (err: any) {
        setHistView(null);
        if (scope === "project" && session.workspaceRoot) {
          const name = workspaceDisplayName(session.workspaceRoot);
          showToast(t("history.failedOpenProject", { name, path: session.workspaceRoot }));
        } else {
          showToast(err?.message || String(err));
        }
      }
    },
    [openGlobalTab, openProjectTab, refreshTabMetas, state.running, resumeSession, t, showToast],
  );

  // Command palette: ⌘K / Ctrl+K opens a fuzzy navigator over commands and
  // recent sessions. Sessions are snapshotted on open so the list is stable
  // while the palette is up.
  const openPalette = useCallback(async () => {
    closeTransientOverlays();
    setPaletteOpen(true);
    setPaletteSessions(await listSessions().catch(() => []));
  }, [closeTransientOverlays, listSessions]);
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        if (!paletteOpen && !e.repeat) void openPalette();
      } else if (e.key === "Escape") {
        setPaletteOpen(false);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [openPalette, paletteOpen]);
  const paletteItems = useMemo<PaletteItem[]>(() => {
    const cmds: PaletteItem[] = [
      { id: "cmd-new", group: t("palette.group.commands"), title: t("palette.cmd.newSession"), keywords: ["new", "新建"], run: () => void handleNewTab() },
      { id: "cmd-history", group: t("palette.group.commands"), title: t("palette.cmd.history"), keywords: ["history", "历史"], run: () => void openAllHistory() },
      { id: "cmd-trash", group: t("palette.group.commands"), title: t("palette.cmd.trash"), keywords: ["trash", "回收站"], run: () => void openTrash() },
      { id: "cmd-settings", group: t("palette.group.commands"), title: t("palette.cmd.settings"), keywords: ["settings", "设置"], run: () => setSettingsTarget("general") },
      { id: "cmd-appearance", group: t("palette.group.commands"), title: t("palette.cmd.appearance"), keywords: ["theme", "appearance", "外观", "主题"], run: () => setSettingsTarget("appearance") },
      { id: "cmd-memory", group: t("palette.group.commands"), title: t("palette.cmd.memory"), keywords: ["memory", "记忆"], run: () => setSettingsTarget("memory") },
      { id: "cmd-models", group: t("palette.group.commands"), title: t("palette.cmd.models"), keywords: ["model", "模型"], run: () => setSettingsTarget("models") },
    ];
    const sessionItems: PaletteItem[] = paletteSessions.slice(0, 12).map((s) => ({
      id: `sess-${s.path}`,
      group: t("palette.group.sessions"),
      title: s.title?.trim() || s.preview || t("history.emptySession"),
      hint: s.workspaceRoot || undefined,
      run: () => void onResumeSession(s),
    }));
    return [...cmds, ...sessionItems];
  }, [t, paletteSessions, handleNewTab, openAllHistory, openTrash, onResumeSession]);
  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  const onDeleteSession = useCallback(
    async (path: string) => {
      if (state.running) return;
      await deleteSession(path);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, deleteSession, listSessions],
  );
  const onRenameSession = useCallback(
    async (path: string, title: string) => {
      if (state.running) return;
      await renameSession(path, title);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, renameSession, listSessions],
  );
  const onRestoreTrashedSession = useCallback(
    async (path: string) => {
      await restoreSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [restoreSession, listTrashedSessions],
  );
  const onPurgeTrashedSession = useCallback(
    async (path: string) => {
      await purgeTrashedSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );
  const onPurgeAllTrashedSessions = useCallback(
    async (paths: string[]) => {
      const uniquePaths = Array.from(new Set(paths));
      for (const path of uniquePaths) {
        await purgeTrashedSession(path);
      }
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );

  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick. A cancel is a no-op.
  const switchFolder = useCallback(async (path?: string) => {
    const picked = path === undefined ? await pickWorkspace() : await switchWorkspace(path);
    if (picked) {
      setProjectRevision((value) => value + 1);
      await refreshTabMetas();
    }
    return picked;
  }, [pickWorkspace, switchWorkspace, refreshTabMetas]);

  const refreshProjectsAndTabs = useCallback(async () => {
    setProjectRevision((value) => value + 1);
    const tabs = await refreshTabMetas();
    if (activeTabId && !tabs.some((tab) => tab.id === activeTabId)) {
      await syncActiveTab(true);
    }
  }, [activeTabId, refreshTabMetas, syncActiveTab]);

  const renameTopic = useCallback(async (topicId: string, title: string) => {
    const nextTitle = title.trim();
    if (!topicId || !nextTitle) return;
    await app.RenameTopic(topicId, nextTitle);
    await refreshProjectsAndTabs();
  }, [refreshProjectsAndTabs]);

  const generateTopicTitle = useCallback(async (scope: string, workspaceRoot: string, topicId: string) => {
    if (!topicId) return;
    try {
      const title = await app.GenerateTopicTitle(scope, workspaceRoot, topicId);
      await refreshProjectsAndTabs();
      showToast(t("projectTree.generateTitleSuccess", { title }));
    } catch (err) {
      showToast(t("projectTree.generateTitleFailed", { err: err instanceof Error ? err.message : String(err) }), "error");
      throw err;
    }
  }, [refreshProjectsAndTabs, showToast, t]);

  const startActiveTopicRename = useCallback(() => {
    if (!activeTab?.topicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(activeTab.topicId);
    setTopicTitleDraft(activeTab.topicTitle || "");
  }, [activeTab?.topicId, activeTab?.topicTitle]);

  const cancelActiveTopicRename = useCallback(() => {
    topicRenameSkipCommitRef.current = true;
    topicRenameCommitHandledRef.current = true;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, []);

  const commitActiveTopicRename = useCallback(async () => {
    if (topicRenameSkipCommitRef.current) {
      topicRenameSkipCommitRef.current = false;
      topicRenameCommitHandledRef.current = false;
      setRenamingTopicId(null);
      return;
    }
    if (topicRenameCommitHandledRef.current) return;
    topicRenameCommitHandledRef.current = true;
    const topicId = renamingTopicId;
    setRenamingTopicId(null);
    if (!topicId) return;
    const nextTitle = topicTitleDraft.trim();
    if (!nextTitle) return;
    try {
      await renameTopic(topicId, nextTitle);
    } catch {
      /* keep the app usable if a stale topic cannot be renamed */
    }
  }, [renameTopic, renamingTopicId, topicTitleDraft]);

  const sidebarExpandBlocked = false;
  const sidebarToggleTitle = responsiveSidebarCollapsed
      ? t("sidebar.expand")
      : t("sidebar.collapse");
  const sidebarNavTooltipDisabled = !responsiveSidebarCollapsed;
  const browserPreviewChrome = typeof window !== "undefined" && !window.runtime;
  const workspacePanelResetWidth = rightDockDetailActive
    ? RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH
    : defaultRightDockTreeWidth();
  const workspacePanelResizeMinWidth = workspacePanelAriaMinWidth(workspacePanelMinWidth, workspacePanelRenderWidth);
  const workspacePanelMaxWidth = rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH;
  const topicbarTitle = topicDisplayTitle(activeTab);
  const topicbarWorkspaceLabel = activeTab ? tabWorkspaceTitle(activeTab) : "";
  const topicbarWorkspacePath = activeTab?.scope === "project" ? activeTab.workspaceRoot || state.meta?.cwd : "";
  const topicbarSubtitleVisible = Boolean(topicbarWorkspaceLabel);
  const topicbarSubtitleTitle = topicbarWorkspacePath || topicbarWorkspaceLabel;
  const runningToolItems = state.items.filter((item): item is Extract<Item, { kind: "tool" }> => item.kind === "tool" && item.status === "running");
  const readingActive = runningToolItems.some((item) => item.readOnly);
  const writingActive = runningToolItems.some((item) => !item.readOnly);
  const thinkingActive = state.running && runningToolItems.length === 0;
  const openWorkbenchView = () => {
    if (workspacePanelRenderable && rightDockMode === "files") {
      closeWorkspacePanel();
      return;
    }
    openWorkspacePanel("files");
  };
  const closeTextEditMenu = () => {
    setTextEditMenuPoint(null);
    setTextEditTarget(null);
    setTextEditSelection("");
  };
  const closeSelectionMenu = () => {
    setSelectionMenuPoint(null);
    setSelectionMenuText("");
  };
  const openTextContextMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    const editable = editableElementFromTarget(event.target);
    if (editable) {
      event.preventDefault();
      event.stopPropagation();
      closeSelectionMenu();
      setTextEditTarget(editable);
      setTextEditSelection(textSelectionForEditable(editable));
      setTextEditMenuPoint(contextMenuPointFromEvent(event));
      return;
    }
    const selected = window.getSelection()?.toString() ?? "";
    if (!selected.trim()) return;
    event.preventDefault();
    event.stopPropagation();
    closeTextEditMenu();
    setSelectionMenuText(selected);
    setSelectionMenuPoint(contextMenuPointFromEvent(event));
  };
  const textEditCanMutate = textEditTarget ? canMutateEditable(textEditTarget) : false;
  const textEditMenuItems: ContextMenuItem[] = [
    {
      key: "paste",
      label: t("composer.editPaste"),
      disabled: !textEditTarget || !textEditCanMutate,
      onSelect: () => {
        const target = textEditTarget;
        closeTextEditMenu();
        if (!target) return;
        target.focus();
        if (target.classList.contains("composer__input")) {
          setComposerPasteRequest((value) => value + 1);
          return;
        }
        void (async () => {
          try {
            const pasted = await navigator.clipboard?.readText();
            if (typeof pasted === "string" && pasted !== "") {
              replaceEditableSelection(target, pasted);
              return;
            }
          } catch {
            /* Fall through to the legacy command path for WebView permission quirks. */
          }
          target.focus();
          document.execCommand("paste");
        })();
      },
    },
    { type: "separator", key: "sep-clipboard" },
    {
      key: "copy",
      label: t("composer.editCopy"),
      disabled: !textEditSelection,
      onSelect: () => {
        const selected = textEditSelection;
        const target = textEditTarget;
        closeTextEditMenu();
        if (!selected) return;
        void (async () => {
          try {
            await navigator.clipboard?.writeText(selected);
          } catch {
            target?.focus();
            document.execCommand("copy");
          }
        })();
      },
    },
    {
      key: "cut",
      label: t("composer.editCut"),
      disabled: !textEditTarget || !textEditCanMutate || !textEditSelection,
      onSelect: () => {
        const selected = textEditSelection;
        const target = textEditTarget;
        closeTextEditMenu();
        if (!target || !selected) return;
        void (async () => {
          try {
            await navigator.clipboard?.writeText(selected);
            replaceEditableSelection(target, "");
          } catch {
            target.focus();
            document.execCommand("cut");
          }
        })();
      },
    },
    { type: "separator", key: "sep-select" },
    {
      key: "select-all",
      label: t("composer.editSelectAll"),
      disabled: !textEditTarget || !editableHasText(textEditTarget),
      onSelect: () => {
        const target = textEditTarget;
        closeTextEditMenu();
        if (target) selectAllEditable(target);
      },
    },
  ];
  const selectionMenuItems: ContextMenuItem[] = [
    {
      key: "copy-selection",
      label: t("common.copy"),
      onSelect: () => {
        const selected = selectionMenuText;
        closeSelectionMenu();
        if (selected) void navigator.clipboard?.writeText(selected);
      },
    },
  ];

  return (
    <ShellExpandProvider>
    <ShellHotkeys />
    <TextSizeHotkeys />
    <div
      ref={appRef}
      className={["app", `app--${desktopPlatform}`, browserPreviewChrome ? "app--browser-preview" : ""].filter(Boolean).join(" ")}
      onContextMenu={openTextContextMenu}
    >
      <div
        className={[
          "layout",
          responsiveSidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          workspacePanelGridOpen ? "layout--workspace-open" : "",
          responsiveWorkspacePanelOpen && workspacePanelMaximized ? "layout--workspace-maximized" : "",
          workspacePanelResizing ? "layout--resizing layout--workspace-resizing" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={layoutStyle}
      >
        <AppChrome
		  uiStyle={desktopUIStyle}
          platform={desktopPlatform}
          browserPreviewChrome={browserPreviewChrome}
          sidebarTogglePressed={sidebarTogglePressed}
          sidebarExpandBlocked={sidebarExpandBlocked}
          sidebarCollapsed={responsiveSidebarCollapsed}
          sidebarToggleTitle={sidebarToggleTitle}
          workspacePanelMaximized={workspacePanelMaximized}
          workspacePanelRenderable={workspacePanelRenderable}
          workspaceTogglePressed={workspaceTogglePressed}
          workspacePanelLabel={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
		  workspaceLabel={topicbarWorkspaceLabel || "O.R.C.A."}
          sessionLabel={topicbarTitle}
          modelLabel={displayedStatusModelLabel || t("status.connecting")}
          statusLights={[
            { key: "read", label: t("topbar.statusRead"), tone: "info", active: readingActive },
            { key: "write", label: t("topbar.statusWrite"), tone: "warn", active: writingActive },
            { key: "think", label: t("topbar.statusThink"), tone: "success", active: thinkingActive },
          ]}
          onOpenProject={() => void switchFolder()}
          onOpenView={openWorkbenchView}
          onOpenSkills={() => setSettingsTarget("skills")}
          onOpenBots={() => setSettingsTarget("bots")}
          onOpenAutomations={() => setAutomationPanelOpen(true)}
          onOpenToolLibrary={() => setToolLibraryPanelOpen(true)}
          onToggleSidebar={toggleSidebar}
          onToggleWorkspacePanel={toggleWorkspacePanel}
          onOpenPalette={() => void openPalette()}
		  onNewSession={() => void handleNewTab()}
		  onExportSession={() => void exportSession("markdown")}
		  onCloseSession={() => { if (activeTabId) void closeTab(activeTabId); }}
		  onOpenSettings={(tab) => setSettingsTarget(tab)}
		  onViewChanges={() => openRightDockMode("changed")}
        />

        <aside className={`sidebar${responsiveSidebarCollapsed ? " sidebar--collapsed" : ""}`} aria-label={t("sidebar.navigation")}>
          <div className="sidebar__brand" aria-hidden={responsiveSidebarCollapsed}>
			<img src={logoSymbol} alt="O.R.C.A." className="sidebar__brand-logo" draggable={false} />
			<span className="sidebar__brand-text">O.R.C.A.</span>
          </div>

          <button
            className="sidebar__new"
            onClick={() => {
              void handleNewTab();
            }}
          >
            <SquarePen size={18} />
            <span>{t("topbar.newSession")}</span>
          </button>

          <section className="sidebar__section sidebar__section--projects">
            <ProjectTree
              activeScope={pendingTopic?.scope ?? activeTab?.scope}
              activeWorkspaceRoot={pendingTopic?.workspaceRoot ?? activeTab?.workspaceRoot}
              activeTopicId={pendingTopic?.topicId ?? activeTab?.topicId}
              loadingTopicId={pendingTopic?.topicId}
              onOpenTopic={handleOpenTopic}
              onOpenProjectHistory={openProjectHistory}
              onCreateTopic={(scope, workspaceRoot) => openBlankSession(scope, scope === "project" ? workspaceRoot : "")}
              onTopicsChanged={refreshProjectsAndTabs}
              onRenameTopic={renameTopic}
              onGenerateTopicTitle={generateTopicTitle}
              refreshSignal={projectRevision}
              onAddProject={async () => {
                await switchFolder();
              }}
            />
          </section>

          <nav className="sidebar__nav">
            <Tooltip label={t("sidebar.allHistory")} fill side="right" disabled={sidebarNavTooltipDisabled}>
              <button
                className="sidebar__navitem"
                onClick={() => void openAllHistory()}
              >
                <History size={15} />
                <span>{t("sidebar.allHistory")}</span>
              </button>
            </Tooltip>
            <Tooltip label={t("sidebar.trash")} fill side="right" disabled={sidebarNavTooltipDisabled}>
              <button
                className="sidebar__navitem"
                onClick={() => void openTrash()}
              >
                <Trash2 size={15} />
                <span>{t("sidebar.trash")}</span>
              </button>
            </Tooltip>
            <Tooltip label={t("topbar.settings")} fill side="right" disabled={sidebarNavTooltipDisabled}>
              <button
                className="sidebar__navitem"
                onClick={() => {
                  closeTransientOverlays();
                  setSettingsTarget("general");
                }}
              >
                <SettingsIcon size={15} />
                <span>{t("topbar.settings")}</span>
              </button>
            </Tooltip>
            {updateInfo?.available && !responsiveSidebarCollapsed && (
              <Tooltip label={t("update.download", { version: updateInfo.latest })} fill side="right">
                <button className="sidebar__update" type="button" onClick={openUpdatePage} aria-label={t("update.download", { version: updateInfo.latest })}>
                  <Download size={15} />
                </button>
              </Tooltip>
            )}
          </nav>

        </aside>
        <button
          className="sidebar-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("sidebar.resize")}
          aria-valuemin={SIDEBAR_MIN_WIDTH}
          aria-valuemax={SIDEBAR_MAX_WIDTH}
          aria-valuenow={sidebarWidth}
          onPointerDown={startSidebarResize}
          onKeyDown={resizeSidebarWithKeyboard}
          onDoubleClick={() => setExpandedSidebarWidth(defaultSidebarWidth())}
        />

        <section className="chat-pane">
          <>
          {desktopUIStyle === "classic" && (
          <header className="topicbar">
            <div className="topicbar__identity">
              <div className="topicbar__title-row">
                {topicbarEditing ? (
                  <div className="topicbar__title-edit">
                    <input
                      autoFocus
                      className="topicbar__title-input"
                      value={topicTitleDraft}
                      onChange={(event) => setTopicTitleDraft(event.target.value)}
                      onKeyDown={(event: KeyboardEvent<HTMLInputElement>) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          void commitActiveTopicRename();
                        }
                        if (event.key === "Escape") {
                          event.preventDefault();
                          cancelActiveTopicRename();
                        }
                      }}
                      onBlur={() => void commitActiveTopicRename()}
                    />
                  </div>
                ) : (
                  <h1 title={topicTitle(activeTab)}>{topicbarTitle}</h1>
                )}
                <Tooltip label={t("topicBar.renameSession")}>
                  <button
                    className="topicbar__icon-btn"
                    type="button"
                    disabled={!activeTab?.topicId || topicbarEditing}
                    onClick={startActiveTopicRename}
                    aria-label={t("topicBar.renameSession")}
                  >
                    <Pencil size={14} />
                  </button>
                </Tooltip>
              </div>
              {topicbarSubtitleVisible && (
                <div className="topicbar__subtitle" title={topicbarSubtitleTitle}>
                  <span>{topicbarWorkspaceLabel}</span>
                </div>
              )}
            </div>
            <div className="topicbar__spacer" />
            <div className="topicbar__actions">
              <CopyButton
                getText={getSessionMarkdown}
                label={t("topicBar.copyAll")}
                showLabel={false}
                className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility topicbar__action--direct-utility"
              />
              <div className={`topicbar__export topicbar__action--direct-utility${topicExportOpen ? " topicbar__export--open" : ""}`}>
                <Tooltip label={t("topicBar.export")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
                    type="button"
                    disabled={!sessionHasContent}
                    aria-label={t("topicBar.export")}
                    aria-haspopup="menu"
                    aria-expanded={topicExportOpen}
                    onClick={() => setTopicExportOpen((open) => !open)}
                  >
                    <Download size={14} />
                  </button>
                </Tooltip>
                {topicExportOpen && (
                  <div className="topicbar__export-menu" role="menu">
                    <button type="button" role="menuitem" onClick={() => void exportSession("markdown")}>
                      <FileText size={13} />
                      <span>{t("topicBar.exportMarkdown")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("json")}>
                      <FileJson size={13} />
                      <span>{t("topicBar.exportJson")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("pdf")}>
                      <FileDown size={13} />
                      <span>{t("topicBar.exportPdf")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("image")}>
                      <FileImage size={13} />
                      <span>{t("topicBar.exportImage")}</span>
                    </button>
                  </div>
                )}
              </div>
              <Tooltip label={t("workspace.changedTab")}>
                <button
                  className="topicbar__action-btn topicbar__action-btn--label topicbar__action--changed"
                  type="button"
                  aria-label={t("workspace.changedTab")}
                  aria-pressed={workspacePanelRenderable && rightDockMode === "changed"}
                  onClick={() => openRightDockMode("changed")}
                >
                  <GitBranch size={14} />
                  <span>{t("workspace.changedTab")}</span>
                </button>
              </Tooltip>
              <div className={`topicbar__overflow${topicOverflowOpen ? " topicbar__overflow--open" : ""}`}>
                <Tooltip label={t("topicBar.more")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
                    type="button"
                    aria-label={t("topicBar.more")}
                    aria-haspopup="menu"
                    aria-expanded={topicOverflowOpen}
                    onClick={() => setTopicOverflowOpen((open) => !open)}
                  >
                    <MoreHorizontal size={15} />
                  </button>
                </Tooltip>
                {topicOverflowOpen && (
                  <div className="topicbar__overflow-menu" role="menu">
                    <button type="button" role="menuitem" onClick={() => { void navigator.clipboard?.writeText(getSessionMarkdown()); setTopicOverflowOpen(false); }}>
                      <FileText size={13} /><span>{t("topicBar.copyAll")}</span>
                    </button>
                    <button type="button" role="menuitem" disabled={!sessionHasContent} onClick={() => { void exportSession("markdown"); setTopicOverflowOpen(false); }}>
                      <Download size={13} /><span>{t("topicBar.exportMarkdown")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => { openRightDockMode("changed"); setTopicOverflowOpen(false); }}>
                      <GitBranch size={13} /><span>{t("workspace.changedTab")}</span>
                    </button>
                  </div>
                )}
              </div>
              <Tooltip label={t("topicBar.command")}>
                <button
                  className="topicbar__action-btn topicbar__action-btn--label topicbar__action-btn--accent"
                  type="button"
                  aria-label={t("topicBar.command")}
                  onClick={() => void openPalette()}
                >
                  <Command size={14} />
                  <span>{t("topicBar.command")}</span>
                </button>
              </Tooltip>
            </div>
          </header>
          )}

          {state.meta?.startupErr && (
            <div className="banner banner--error">{t("topbar.startupError", { msg: state.meta.startupErr })}</div>
          )}

          <main className="main">
            {(state.loading && !state.meta) || (state.meta?.ready === false && !state.meta?.startupErr) ? (
              <div className="loading-screen">
                <div className="loading-screen__spinner" />
                <span className="loading-screen__text">{t("common.loading")}</span>
              </div>
            ) : (
              <Transcript
                items={state.items}
                live={state.live}
                running={state.running}
                footerHeight={footerHeight}
                onPrompt={send}
                onEditUserMessage={replaceComposerText}
                onRewind={handleMessageAction}
                checkpoints={state.checkpoints}
                actionPending={state.messageAction != null}
                rewindDisabled={state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
                processDisplayMode={processDisplayMode}
                activityIndicatorEnabled={activityIndicatorEnabled}
                paused={Boolean(state.meta?.paused)}
                hydrating={state.hydrating}
                jobs={state.jobs}
              />
            )}
          </main>

          <footer className="footer" ref={footerRef}>
            <div className="footer-shelves">
              {automationAccessRequired && (
                <div className="automation-access-prompt" data-ui-surface="panel" role="alert">
                  <div className="automation-access-prompt__copy">
                    <strong>{t("automation.access.title")}</strong>
                    <span>{t("automation.access.hint")}</span>
                  </div>
                  <button
                    type="button"
                    className="btn btn--primary"
                    onClick={() => void app.SetAutomationFullAccess(true).then(() => refreshMeta()).then(() => refreshTabMetas())}
                  >
                    {t("automation.access.confirm")}
                  </button>
                </div>
              )}
              {activeQueuedPrompts.length > 0 && (
                <div className="queued-prompts" aria-label={t("queuedPrompts.title")} data-ui-surface="panel">
                <div className="queued-prompts__head">
                  <span>{t("queuedPrompts.title")}</span>
                  <strong>{activeQueuedPrompts.length}</strong>
                </div>
                <div className="queued-prompts__list">
                  {activeQueuedPrompts.map((prompt) => (
                    <div className="queued-prompts__item" key={prompt.id}>
                      <span className="queued-prompts__text" title={prompt.displayText}>{prompt.displayText}</span>
                      <div className="queued-prompts__actions">
                        <button type="button" className="queued-prompts__guide" onClick={() => guideQueuedPrompt(prompt)}>
                          {t("queuedPrompts.guide")}
                        </button>
                        <button type="button" className="queued-prompts__remove" aria-label={t("queuedPrompts.remove")} onClick={() => removeQueuedPrompt(prompt.id)}>
                          <Trash2 size={13} />
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
                </div>
              )}
              {showTodos && <TodoPanel todoId={todoItem!.id} todos={todos} onDismiss={() => setDismissedTodo(todoItem!.id)} />}
              {state.approval && (
                <ApprovalModal
                approval={state.approval}
                onAnswer={(allow, session, persist) => {
                  // Approving an exit_plan_mode plan leaves plan mode; sync the
                  // tab-local indicator and persisted safe mode immediately.
                  if (state.approval!.tool === "exit_plan_mode" && allow) applyCollaborationMode("normal");
                  approve(state.approval!.id, allow, session, persist);
                }}
                onRevisePlan={(text) => {
                  setPendingPlanRevision(text);
                  approve(state.approval!.id, false, false, false);
                }}
                onExitPlan={() => {
                  applyCollaborationMode("normal");
                  approve(state.approval!.id, false, false, false);
                }}
                />
              )}
              {state.ask && (
                <AskCard
                ask={state.ask}
                onAnswer={answerQuestion}
                onDismiss={() => answerQuestion(state.ask!.id, [])}
                />
              )}
              {clearContextPending && (
                <ClearContextCard
                onCancel={cancelClearContext}
                onConfirm={() => {
                  void confirmClearContext();
                }}
                />
              )}
            </div>
            <Composer
              key={activeTabId || "global-composer"}
			  uiStyle={desktopUIStyle}
              running={state.running}
              collaborationMode={collaborationMode}
              askWorkflowEnabled={askWorkflowEnabled}
              stepThinkingEnabled={stepThinkingEnabled}
              toolApprovalMode={toolApprovalMode}
              promptMode={promptMode}
			  promptModes={automationConversation ? [] : productCapabilities.promptModes}
			  promptModeLocked={false}
              promptModeSwitching={promptModeSwitching}
              runtimeSwitch={state.runtimeSwitch}
              cancelRequested={state.cancelRequested}
              showToolApprovalControls={!automationConversation}
              paused={Boolean(state.meta?.paused)}
              goal={goal}
              cwd={state.meta?.cwd}
              modelLabel={displayedModelLabel}
              tabId={activeTabId}
              effort={displayedEffort}
              onSend={handleSend}
              onGuide={handleGuide}
              onCancel={cancel}
              onCycleMode={cycleMode}
              onSetMode={applyMode}
              onSetCollaborationMode={applyCollaborationMode}
              onSetToolApprovalMode={applyToolApprovalMode}
              onSetAskWorkflow={applyAskWorkflow}
              onSetStepThinking={applyStepThinking}
              onSetPromptMode={(mode) => void applyPromptMode(mode)}
              onTogglePause={() => void togglePause()}
              onToggleYoloApprovalMode={toggleYoloApprovalMode}
              onSetGoal={startGoal}
              onClearGoal={() => applyGoal("")}
              onSwitchModel={switchModel}
              onSetEffort={(level) => void switchEffort(level)}
              insertRequest={composerInsertRequest}
              pasteRequest={composerPasteRequest}
              disabled={state.meta?.ready === false || state.meta?.readOnly === true || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
              decisionPending={state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
              ready={state.meta?.ready === true}
              turnStartAt={state.turnStartAt}
              turnTokens={state.turnTokens}
              retry={state.retry}
              transientDismissSignal={transientOverlayDismissSignal}
            />
            <StatusBar
              uiStyle={desktopUIStyle}
              context={state.context}
              usage={state.usage}
              balance={state.balance}
              jobs={state.jobs}
              running={state.running}
              collaborationMode={collaborationMode}
              askWorkflowEnabled={askWorkflowEnabled}
              stepThinkingEnabled={stepThinkingEnabled}
              toolApprovalMode={toolApprovalMode}
              sessionTokens={state.sessionTokens}
              cost={state.sessionCost}
              currency={state.sessionCurrency}
              modelLabel={displayedStatusModelLabel}
              updateInfo={responsiveSidebarCollapsed ? updateInfo : null}
              onOpenUpdate={openUpdatePage}
            />
          </footer>
          </>
        </section>

        {workspacePanelGridOpen && (
          <button
            className="workspace-panel-resizer"
            type="button"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("rightDock.resize")}
            aria-valuemin={workspacePanelResizeMinWidth}
            aria-valuemax={Math.max(workspacePanelMaxWidth, workspacePanelRenderWidth)}
            aria-valuenow={workspacePanelRenderWidth}
            onPointerDown={startWorkspacePanelResize}
            onKeyDown={resizeWorkspacePanelWithKeyboard}
            onDoubleClick={() => setSavedWorkspacePanelWidth(workspacePanelResetWidth)}
          />
        )}

        {workspacePanelRenderable && (
          <aside
            className={[
              "workbench-dock",
              `workbench-dock--${rightDockMode}`,
            ].join(" ")}
            aria-label={t("rightDock.workbench")}
          >
            <div className="workbench-dock__tools">
              <div className="workbench-dock__tabs" role="tablist" aria-label={t("rightDock.views")}>
                {SHOW_CONTEXT_DOCK && (
                  <button
                    type="button"
                    role="tab"
                    aria-selected={rightDockMode === "context"}
                    className={`workbench-dock__tab${rightDockMode === "context" ? " workbench-dock__tab--active" : ""}`}
                    onClick={() => openRightDockMode("context")}
                    title={t("rightDock.overview")}
                  >
                    <Activity size={13} />
                    <span className="workbench-dock__tab-label">{t("rightDock.overview")}</span>
                  </button>
                )}
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "files"}
                  className={`workbench-dock__tab${rightDockMode === "files" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("files")}
                  title={t("workspace.filesTab")}
                >
                  <FileText size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.filesTab")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "changed"}
                  className={`workbench-dock__tab${rightDockMode === "changed" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("changed")}
                  title={t("workspace.changedTab")}
                >
                  <GitBranch size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.changedTab")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "sideChat"}
                  className={`workbench-dock__tab${rightDockMode === "sideChat" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("sideChat")}
                  title={t("rightDock.sideChat")}
                >
                  <MessageSquareText size={13} />
                  <span className="workbench-dock__tab-label">{t("rightDock.sideChat")}</span>
                </button>
              </div>
            </div>
            <div className="workbench-dock__body">
              {rightDockMode === "context" ? (
                <ContextPanel
                  tabId={activeTabId}
                  context={state.context}
                  usage={state.usage}
                  sessionTokens={state.sessionTokens}
                  sessionCost={state.sessionCost}
                  sessionCurrency={state.sessionCurrency}
                  refreshKey={dockRefreshKey}
                  onOpenWorkspaceMode={openRightDockMode}
                  onOpenWorkspaceFile={openRightDockFile}
                  onOpenWorkspaceFileList={openRightDockFileList}
                  onOpenWorkspaceChangeList={openRightDockChangeList}
                  onOpenWorkspaceChangeFile={openRightDockChangeFile}
                />
              ) : rightDockMode === "sideChat" ? (
                <SideChatPanel tabId={activeTabId} />
              ) : (
                <WorkspacePanel
                  open={workspacePanelRenderable}
                  cwd={state.meta?.cwd}
                  maximized={workspacePanelMaximized}
                  panelWidth={workspacePanelRenderWidth}
                  onClose={() => setWorkspacePanel(false)}
                  onToggleMaximized={() => {
                    closeTransientOverlays();
                    setWorkspacePanelMaximized((value) => !value);
                  }}
                  onPreviewModeChange={handleWorkspacePreviewModeChange}
                  onAddToChat={addWorkspaceTextToComposer}
                  onRequestPanelWidth={ensureWorkspacePanelWidth}
                  refreshKey={dockRefreshKey}
                  initialViewMode={rightDockMode === "changed" ? "changed" : "files"}
                  revealPathRequest={workspaceRevealRequest}
                  changeRevealRequest={workspaceChangeRevealRequest}
                  fileListRequest={workspaceFileListRequest}
                  changeListRequest={workspaceChangeListRequest}
                  showViewTabs={false}
                />
              )}
            </div>
          </aside>
        )}
      </div>

      {updateInfo?.available && shouldShowUpdate(updateDismissal, updateInfo.latest) && (
        <UpdateBanner
          info={updateInfo}
          platform={desktopPlatform}
          onDismiss={() => setUpdateDismissal((current) => dismissUpdate(current, updateInfo.latest))}
        />
      )}

      {histView !== null && (
        <HistoryPanel
          kind={histView.kind}
          sessions={histView.sessions}
          running={state.running}
          onResume={onResumeSession}
          onPreview={previewSession}
          onDelete={onDeleteSession}
          onRename={onRenameSession}
          onRestore={onRestoreTrashedSession}
          onPurge={onPurgeTrashedSession}
          onPurgeAll={onPurgeAllTrashedSessions}
          onClose={closeHistory}
        />
      )}

      {settingsTarget !== null && (
        <SettingsPanel
          initialTab={settingsTarget}
          productCapabilities={productCapabilities}
          onClose={() => setSettingsTarget(null)}
          onChanged={() => {
            void refreshMeta();
            void app.Settings()
              .then(applyDesktopPreferences)
              .catch((e) => console.warn("desktop preferences refresh failed", e));
          }}
        />
      )}

      {automationPanelOpen && (
        <AutomationPanel onClose={() => setAutomationPanelOpen(false)} />
      )}

      {toolLibraryPanelOpen && (
        <ToolLibraryPanel onClose={() => setToolLibraryPanelOpen(false)} />
      )}

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        items={paletteItems}
        placeholder={t("palette.placeholder")}
        emptyText={t("palette.empty")}
      />

      <NewSessionChooser
        open={newSessionChooserOpen}
        onClose={() => setNewSessionChooserOpen(false)}
        onChoose={handleNewSessionChoice}
        onPickProjectFolder={async () => {
          const picked = await switchFolder();
          if (picked) {
            setNewSessionChooserOpen(false);
          }
        }}
      />

      {startupSplashVisible && (
        <StartupSplash hold={startupSplashHold} onDone={() => setStartupSplashVisible(false)} />
      )}

      {needsOnboarding && !startupSplashVisible && <OnboardingOverlay onComplete={() => setNeedsOnboarding(false)} />}
      <ContextMenu
        open={Boolean(textEditMenuPoint)}
        point={textEditMenuPoint}
        items={textEditMenuItems}
        onClose={closeTextEditMenu}
        minWidth={132}
        ariaLabel={t("composer.editMenu")}
      />
      <ContextMenu
        open={Boolean(selectionMenuPoint)}
        point={selectionMenuPoint}
        items={selectionMenuItems}
        onClose={closeSelectionMenu}
        minWidth={120}
        ariaLabel={t("common.copy")}
      />
    </div>
    </ShellExpandProvider>
  );
}
