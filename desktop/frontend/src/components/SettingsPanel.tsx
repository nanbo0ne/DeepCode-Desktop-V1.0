import { useEffect, useMemo, useRef, useState, type Dispatch, type ReactNode, type SetStateAction } from "react";
import { Check, ChevronDown, Download, Pause, Play, RefreshCw, ShieldCheck, Square, Trash2, X } from "lucide-react";
import { asArray } from "../lib/array";
import { useDeferredClose } from "../lib/useMountTransition";
import { app, onComputerUseChanged, onLocalAIChanged, openExternal } from "../lib/bridge";
import { normalizeLangPref, useI18n, useT, type DictKey, type LangPref } from "../lib/i18n";
import { mergedFetchedProviderModels, providerDefaultModel, providerModelCandidates } from "../lib/providerModels";
import { TEXT_SIZES, applyTextSize, getTextSize, type TextSize } from "../lib/textSize";
import { FONT_FAMILIES, applyFontFamily, getFontFamily, type FontFamily } from "../lib/fontFamily";
import { persistUIStyle, UI_STYLES, type UIStyle } from "../lib/uiStyle";
import { checkDesktopUpdate } from "../lib/updateCheck";
import type { BotConnectionView, BotInstallStartResult, BotSettingsView, ComputerUseState, LocalAICatalogView, NetworkView, ProcessDisplayMode, ProductCapabilities, PromptMode, ProviderView, SettingsTab, SettingsView, VisionCapability } from "../lib/types";
import { normalizeLocalAICatalog } from "../lib/localAI";
import { localDownloadActions, wrappedFocusIndex, type LocalDownloadAction } from "../lib/settingsPanelState";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import { InlineConfirmButton } from "./InlineConfirmButton";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";
import { MCPServersSettingsPage, SkillsSettingsPage } from "./CapabilitiesPanel";
import { MemorySettingsPage } from "./MemoryPanel";
import { ModalCloseButton } from "./ModalCloseButton";

const BOT_SETTINGS_ENABLED = true;
const SETTINGS_TABS: SettingsTab[] = [
  "general",
  "models",
  ...(BOT_SETTINGS_ENABLED ? (["bots"] as SettingsTab[]) : []),
  "mcp",
  "skills",
  "memory",
  "permissions",
  "sandbox",
  "network",
	"localAI",
	"computer",
  "appearance",
	"about",
];

// SettingsPanel is the desktop settings centre — a centred modal with left
// navigation and a right content area. It hosts all settings pages plus MCP,
// Skills, and Memory management, replacing the old per-feature drawers.
export function SettingsPanel({ onClose, onChanged, initialTab, productCapabilities }: { onClose: () => void; onChanged: () => void; initialTab?: SettingsTab; productCapabilities: ProductCapabilities }) {
  const t = useT();
  const [s, setS] = useState<SettingsView | null>(null);
  const [settingsLoading, setSettingsLoading] = useState(true);
  const [settingsError, setSettingsError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [textSize, setTextSizeState] = useState<TextSize>(getTextSize());
  const [fontFamily, setFontFamilyState] = useState<FontFamily>(getFontFamily());
	const [restartStyle, setRestartStyle] = useState<UIStyle | null>(null);
  const [tab, setTab] = useState<SettingsTab>(normalizeInitialSettingsTab(initialTab));
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const returnFocusRef = useRef<HTMLElement | null>(null);
  // Play the modal exit animation, then let the parent unmount us.
  const { status, requestClose } = useDeferredClose(onClose, 240);

  const reload = async () => {
    setSettingsLoading(true);
    try {
      const next = normalizeSettingsView(await app.Settings());
      setS(next);
      setSettingsError(null);
      return true;
    } catch {
      setSettingsError(t("settings.loadFailed"));
      return false;
    } finally {
      setSettingsLoading(false);
    }
  };
  useEffect(() => {
    void reload();
    if (initialTab) setTab(normalizeInitialSettingsTab(initialTab));
  }, [initialTab]);

  // Keep focus inside the modal and make the rest of the app inert while it is
  // open. The element that opened settings receives focus after close.
  useEffect(() => {
    const active = document.activeElement;
    returnFocusRef.current = active instanceof HTMLElement ? active : null;
    const backdrop = dialogRef.current?.parentElement;
    const appRoot = backdrop?.parentElement;
    const background = appRoot
      ? Array.from(appRoot.children).filter((element) => element !== backdrop) as HTMLElement[]
      : [];
    const previousInert = background.map((element) => element.inert);
    background.forEach((element) => { element.inert = true; });
    const frame = requestAnimationFrame(() => {
      const first = dialogRef.current?.querySelector<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      );
      (first || dialogRef.current)?.focus();
    });
    return () => {
      cancelAnimationFrame(frame);
      background.forEach((element, index) => { element.inert = previousInert[index] ?? false; });
      if (returnFocusRef.current?.isConnected) requestAnimationFrame(() => returnFocusRef.current?.focus());
    };
  }, []);

  const trapFocus = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "Tab") return;
    const focusable = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>(
      'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ) || []).filter((element) => element.offsetParent !== null);
    const currentIndex = focusable.indexOf(document.activeElement as HTMLElement);
    const nextIndex = wrappedFocusIndex(currentIndex, focusable.length, event.shiftKey);
    if (nextIndex < 0) {
      event.preventDefault();
      dialogRef.current?.focus();
    } else if (currentIndex < 0 || (event.shiftKey && currentIndex === 0) || (!event.shiftKey && currentIndex === focusable.length - 1)) {
      event.preventDefault();
      focusable[nextIndex]?.focus();
    }
  };

  // Model pickers use a body portal. When one is open, include its controls in
  // the same focus scope so Tab can return to the settings dialog.
  useEffect(() => {
    const onPortalKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key !== "Tab") return;
      const popover = document.querySelector<HTMLElement>("[data-anchored-popover='active']:not([aria-hidden='true'])");
      if (!popover || !popover.contains(document.activeElement)) return;
      const dialogControls = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ) || []).filter((element) => element.offsetParent !== null);
      const popoverControls = Array.from(popover.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      )).filter((element) => element.offsetParent !== null);
      const focusable = [...dialogControls, ...popoverControls];
      const nextIndex = wrappedFocusIndex(focusable.indexOf(document.activeElement as HTMLElement), focusable.length, event.shiftKey);
      if (nextIndex >= 0) {
        event.preventDefault();
        focusable[nextIndex]?.focus();
      }
    };
    document.addEventListener("keydown", onPortalKeyDown, true);
    return () => document.removeEventListener("keydown", onPortalKeyDown, true);
  }, []);
  // apply runs a mutation, re-reads settings, and refreshes the topbar/model.
  const apply = async (fn: () => Promise<void>) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      await reload();
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  };
  const backgroundApply = async (fn: () => Promise<void>) => {
    setErr(null);
    try {
      await fn();
      await reload();
      onChanged();
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
    }
  };

  // Close on Esc
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("[data-anchored-popover='active']")) requestClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [requestClose]);

  // The settings-reliant pages (general, models, network, permissions,
  // sandbox, appearance) need SettingsView loaded. MCP, Skills, and Memory
  // load their own data and render regardless.
  const needsSettings = tab === "general" || tab === "models" || (BOT_SETTINGS_ENABLED && tab === "bots") || tab === "network" || tab === "permissions" || tab === "sandbox" || tab === "computer" || tab === "appearance";

  return (
    <div className="management-modal-backdrop settings-modal-backdrop" data-state={status} onClick={(e) => { if (e.target === e.currentTarget) requestClose(); }}>
      <div ref={dialogRef} className="management-modal settings-modal" data-state={status} role="dialog" aria-modal="true" aria-labelledby="settings-dialog-title" aria-busy={settingsLoading} tabIndex={-1} onKeyDown={trapFocus}>
        <header className="management-modal__head settings-modal__head">
          <div id="settings-dialog-title" className="management-modal__title settings-modal__title">{t("settings.title")}</div>
          <ModalCloseButton label={t("common.close")} onClick={requestClose} />
        </header>

        <div className="settings-center">
          <nav className="settings-center__nav" aria-label={t("settings.title")}>
            {SETTINGS_TABS.map((id) => (
              <button
                key={id}
                className={`settings-center__navitem${tab === id ? " settings-center__navitem--active" : ""}`}
                onClick={() => setTab(id)}
              >
                <span>{settingsTabLabel(id, t)}</span>
                {s && <small>{settingsTabMeta(id, s, t)}</small>}
              </button>
            ))}
          </nav>
          <main className="settings-center__content">
            {needsSettings && settingsError && <div className="banner banner--error" role="alert"><span>{settingsError}</span>{!s && <button type="button" className="btn btn--ghost" disabled={settingsLoading} onClick={() => void reload()}>{t("settings.retryLoad")}</button>}</div>}
            {needsSettings && err && <div className="banner banner--error" role="alert">{err}</div>}
            {needsSettings && !s ? (
              <div className="empty">{settingsLoading ? t("settings.loading") : t("settings.loadFailed")}</div>
            ) : (
              <>
                {tab === "general" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><GeneralSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "models" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><ModelsSection s={s} busy={busy} apply={apply} backgroundApply={backgroundApply} /></SettingsPageShell>}
                {BOT_SETTINGS_ENABLED && tab === "bots" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><BotsSection s={s} busy={busy} apply={apply} promptModes={productCapabilities.promptModes} /></SettingsPageShell>}
                {tab === "mcp" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><MCPServersSettingsPage /></SettingsPageShell>}
                {tab === "skills" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><SkillsSettingsPage /></SettingsPageShell>}
                {tab === "memory" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><MemorySettingsPage assistantMemoryEnabled={productCapabilities.assistantMemoryEnabled || productCapabilities.automationWorkspaceEnabled === true} /></SettingsPageShell>}
                {tab === "permissions" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><PermissionsSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "sandbox" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><SandboxSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "network" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><NetworkSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
				{tab === "localAI" && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><LocalAISection /></SettingsPageShell>}
				{tab === "computer" && s && <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}><ComputerUseSection s={s} busy={busy} apply={apply} /></SettingsPageShell>}
                {tab === "appearance" && s && (
                  <SettingsPageShell key={tab} s={s} tab={tab} busy={busy} apply={apply}>
                    <AppearanceSection
                      busy={busy}
                      uiStyle={s.desktopUIStyle}
                      textSize={textSize}
                      fontFamily={fontFamily}
                      onTextSize={(size) => {
                        applyTextSize(size);
                        setTextSizeState(size);
                      }}
                      onFontFamily={(font) => {
                        applyFontFamily(font);
                        setFontFamilyState(font);
                      }}
                      onUIStyle={(style) => {
                        void apply(async () => {
							await app.SetDesktopUIStyle(style);
							persistUIStyle(style);
							setRestartStyle(style);
                        });
                      }}
					  restartStyle={restartStyle}
					  onRestartLater={() => setRestartStyle(null)}
					  onRestartNow={() => void app.RestartApp().catch((error) => setErr(String((error as Error)?.message ?? error)))}
                    />
                  </SettingsPageShell>
                )}
				{tab === "about" && <SettingsPageShell key={tab} s={s} tab={tab} busy={false} apply={apply}><AboutSection /></SettingsPageShell>}
              </>
            )}
          </main>
        </div>
      </div>
    </div>
  );
}

function SettingsPageShell({ s: _s, tab, children }: { s: SettingsView | null; tab: SettingsTab; busy: boolean; apply: (fn: () => Promise<void>) => Promise<void>; children: ReactNode }) {
  const t = useT();
  const descKey = `settings.pageDesc.${tab}` as keyof typeof import("../locales/en").en;
  const desc = t(descKey as any);
  return (
    <div className={`settings-page settings-page--${settingsPageKind(tab)}`}>
      <div className="settings-page__header">
        <h2 className="settings-page__title">{settingsTabPageTitle(tab, t)}</h2>
        {typeof desc === "string" && desc !== `settings.pageDesc.${tab}` && <p className="settings-page__desc">{desc}</p>}
      </div>
      {children}
    </div>
  );
}

function settingsPageKind(tab: SettingsTab): "form" | "manager" {
  switch (tab) {
    case "models":
    case "mcp":
    case "skills":
	case "memory":
	case "localAI":
	case "computer":
		return "manager";
    default:
      return "form";
  }
}

function SettingsSection({
  title,
  description,
  actions,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="settings-section">
      <div className="settings-section__head">
        <div>
          <div className="settings-section__title">{title}</div>
          {description && <div className="settings-section__desc">{description}</div>}
        </div>
        {actions && <div className="settings-section__actions">{actions}</div>}
      </div>
      <div className="settings-section__body">{children}</div>
    </section>
  );
}

function SettingsField({
  label,
  hint,
  children,
  className,
  stacked = false,
}: {
  label: ReactNode;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
  stacked?: boolean;
}) {
  return (
    <div className={`settings-field${stacked ? " settings-field--stacked" : ""}${className ? ` ${className}` : ""}`}>
      <div className="settings-field__copy">
        <div className="settings-field__label">{label}</div>
        {hint && (
          <div className="settings-field__hint">
            <SettingsHint hint={hint} />
          </div>
        )}
      </div>
      <div className="settings-field__control">{children}</div>
    </div>
  );
}

function SettingsHint({ hint }: { hint: ReactNode }) {
  if (typeof hint === "string" || typeof hint === "number") {
    const label = String(hint);
    return (
      <Tooltip label={label} fill block className="settings-field__hint-tooltip">
        <span className="settings-field__hint-line">{label}</span>
      </Tooltip>
    );
  }
  return hint;
}

function settingsTabPageTitle(id: SettingsTab, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "mcp": return t("settings.tab.mcp");
    case "skills": return t("settings.tab.skills");
    case "memory": return t("settings.tab.memory");
    default: return settingsTabLabel(id, t);
  }
}

type SectionProps = {
  s: SettingsView;
  busy: boolean;
  apply: (fn: () => Promise<void>) => Promise<void>;
};

type ModelsSectionProps = SectionProps & {
  backgroundApply: (fn: () => Promise<void>) => Promise<void>;
};

function LocalAISection() {
  const t = useT();
  const [catalog, setCatalog] = useState<LocalAICatalogView | null>(null);
  const catalogRef = useRef<LocalAICatalogView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const pendingActionRef = useRef(false);
  const [retryAction, setRetryAction] = useState<(() => void) | null>(null);
  const [loading, setLoading] = useState(true);
  const reload = async () => {
    setError(null);
    setRetryAction(null);
    setLoading(true);
    try {
      const next = await app.GetLocalAICatalog();
      const normalized = normalizeLocalAICatalog(next);
      catalogRef.current = normalized;
      setCatalog(normalized);
      return true;
    } catch {
      setError(catalogRef.current ? t("settings.localAI.refreshFailed") : t("settings.localAI.loadFailed"));
      if (catalogRef.current) setRetryAction(() => () => { void reload(); });
      return false;
    } finally {
      setLoading(false);
    }
  };
  const runLocalAction = async (label: string, operation: () => Promise<unknown>) => {
    if (pendingActionRef.current) return;
    pendingActionRef.current = true;
    setPendingAction(label);
    setError(null);
    setRetryAction(null);
    try {
      await operation();
      const refreshed = await reload();
      if (!refreshed) setRetryAction(() => () => { void reload(); });
    } catch {
      setError(t("settings.localAI.operationFailed", { action: label }));
      setRetryAction(() => () => { void runLocalAction(label, operation); });
    } finally {
      pendingActionRef.current = false;
      setPendingAction(null);
    }
  };
  useEffect(() => {
    void reload();
    return onLocalAIChanged(() => { void reload(); });
  }, []);
  if (!catalog) return error ? (
    <div className="banner banner--error" role="alert">
      <span>{error}</span>
      <button type="button" className="btn btn--ghost" disabled={loading} onClick={() => void reload()}>{t("settings.retryLoad")}</button>
    </div>
  ) : <div className="empty">{t("settings.localAI.loading")}</div>;
  const installed = new Set(catalog.installedModels.map((model) => model.id));
  const gpuSummary = catalog.hardware.gpuDetectionFailed
    ? t("settings.localAI.gpuDetectionFailed")
    : catalog.hardware.gpus.map((gpu) => `${gpu.name} ${Math.round(gpu.dedicatedMiB / 1024)}GB`).join("、") || t("settings.localAI.noDiscreteGpu");
  return (
    <>
      {error && <div className="banner banner--error" role="alert"><span>{error}</span>{retryAction && <button type="button" className="btn btn--ghost" disabled={pendingAction !== null} onClick={retryAction}>{t("settings.retryLoad")}</button>}</div>}
      {!catalog.supported && <div className="banner">{t("settings.localAI.platformUnsupported")}</div>}
      <SettingsSection title={t("settings.localAI.hardwareRuntime")} description={`${gpuSummary} · ${t("settings.localAI.memory", { n: Math.round(catalog.hardware.memoryTotalMiB / 1024) })}`}>
        <div className="settings-inline-actions">
          <span className="settings-value">{t("settings.localAI.recommended", { model: catalog.hardware.recommendedModel || t("settings.localAI.noRecommendation") })}</span>
          {catalog.runtime ? <button className="btn btn--ghost" disabled={pendingAction !== null} onClick={() => void runLocalAction(t("settings.localAI.stopRuntime"), () => app.StopLocalRuntime())}><Square size={14} />{t("settings.localAI.stopRuntime")}</button> : (
            <button className="btn btn--primary" disabled={!catalog.supported || pendingAction !== null} onClick={() => void runLocalAction(t("settings.localAI.installRuntime"), () => app.StartLocalRuntimeInstall(catalog.hardware.recommendedRuntime))}><Download size={14} />{t("settings.localAI.installRuntime")}</button>
          )}
        </div>
        <div className="settings-field__hint-line">{t("settings.localAI.runtimeHint")}</div>
      </SettingsSection>
      <SettingsSection title={t("settings.localAI.modelLibrary")} description={t("settings.localAI.directory", { path: catalog.modelsDirectory })}>
        {catalog.models.map((model) => {
          const isInstalled = installed.has(model.id);
          const task = catalog.downloads.find((item) => item.targetId === model.id);
          return <div className="settings-list-row" key={model.id}>
            <div className="settings-list-row__main"><strong>{model.name}</strong><span>{model.description}</span><small>{model.vision ? t("settings.localAI.vision") : t("settings.localAI.text")} · {model.toolUse ? t("settings.localAI.tools") : t("settings.localAI.chat")} · {model.license}</small></div>
            <div className="settings-list-row__actions">
              {task && <LocalDownloadStatus task={task} onAction={(action) => {
                if (action === "pause") return runLocalAction(t("settings.localAI.pause"), () => app.PauseLocalDownload(task.id));
                if (action === "resume") return runLocalAction(t("settings.localAI.resume"), () => app.ResumeLocalDownload(task.id));
                if (action === "cancel") return runLocalAction(t("settings.localAI.cancel"), () => app.CancelLocalDownload(task.id));
                const label = action === "retry" ? t("settings.localAI.retryDownload") : t("settings.localAI.redownload");
                return runLocalAction(label, () => app.StartLocalModelDownload(task.targetId));
              }} pendingAction={pendingAction !== null} />}
              {isInstalled ? <button className="btn btn--ghost" disabled={pendingAction !== null} onClick={() => void runLocalAction(t("common.delete"), () => app.DeleteLocalModel(model.id))}><Trash2 size={14} />{t("common.delete")}</button> : <button className="btn btn--primary" disabled={!catalog.supported || !!task || pendingAction !== null} onClick={() => void runLocalAction(t("settings.localAI.download"), () => app.StartLocalModelDownload(model.id))}><Download size={14} />{t("settings.localAI.download")}</button>}
            </div>
          </div>;
        })}
      </SettingsSection>
    </>
  );
}

function LocalDownloadStatus({ task, onAction, pendingAction }: { task: LocalAICatalogView["downloads"][number]; onAction: (action: LocalDownloadAction) => Promise<void>; pendingAction: boolean }) {
  const t = useT();
  const progress = task.totalBytes > 0 ? Math.min(100, Math.round((task.downloadedBytes / task.totalBytes) * 100)) : 0;
  const formatSize = (value: number) => value >= 1024 ** 3 ? `${(value / 1024 ** 3).toFixed(1)} GB` : `${Math.max(0, Math.round(value / 1024 ** 2))} MB`;
  const formatEta = (seconds: number) => seconds > 3600 ? `${Math.ceil(seconds / 3600)}h` : seconds > 60 ? `${Math.ceil(seconds / 60)}m` : `${Math.max(0, Math.ceil(seconds))}s`;
  const actions = localDownloadActions(task.state);
  return <div className="local-download-status">
    <div className="local-download-status__meta"><span>{task.state === "completed" ? t("settings.localAI.verified") : task.state === "failed" ? t("settings.localAI.downloadFailed") : `${progress}%`}</span><span>{task.source || t("settings.localAI.mirror")}</span></div>
    <div className="local-download-status__bar" role="progressbar" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100}><span style={{ width: `${progress}%` }} /></div>
    <div className="local-download-status__meta"><span>{formatSize(task.downloadedBytes)} / {formatSize(task.totalBytes)}</span><span>{task.bytesPerSecond > 0 ? t("settings.localAI.perSecond", { size: formatSize(task.bytesPerSecond) }) : task.error || (task.etaSeconds > 0 ? t("settings.localAI.remaining", { time: formatEta(task.etaSeconds) }) : t("settings.localAI.preparing"))}</span></div>
    {actions.length > 0 && <div className="local-download-status__controls">
      {actions.includes("pause") && <button className="icon-button" disabled={pendingAction} title={t("settings.localAI.pause")} aria-label={t("settings.localAI.pause")} onClick={() => void onAction("pause")}><Pause size={13} /></button>}
      {actions.includes("resume") && <button className="icon-button" disabled={pendingAction} title={t("settings.localAI.resume")} aria-label={t("settings.localAI.resume")} onClick={() => void onAction("resume")}><Play size={13} /></button>}
      {actions.includes("retry") && <button className="btn btn--small" disabled={pendingAction} onClick={() => void onAction("retry")}><RefreshCw size={13} />{t("settings.localAI.retryDownload")}</button>}
      {actions.includes("redownload") && <button className="btn btn--small" disabled={pendingAction} onClick={() => void onAction("redownload")}><Download size={13} />{t("settings.localAI.redownload")}</button>}
      {actions.includes("cancel") && <button className="icon-button icon-button--danger" disabled={pendingAction} title={t("settings.localAI.cancel")} aria-label={t("settings.localAI.cancel")} onClick={() => void onAction("cancel")}><X size={13} /></button>}
    </div>}
  </div>;
}

function ComputerUseSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const [state, setState] = useState<ComputerUseState | null>(null);
  const reload = () => { void app.GetComputerUseState().then(setState).catch(() => {}); };
  useEffect(() => { reload(); return onComputerUseChanged(reload); }, []);
  const modelOptions = s.providers.flatMap((provider) => provider.models.map((model) => `${provider.name}/${model}`));
  return <>
    <SettingsSection title={t("settings.computer.title")} description={t("settings.computer.description")}>
      <SettingsField label={t("settings.computer.controlModel")} hint={t("settings.computer.modelHint")}>
        <select className="settings-select" disabled={busy} value={s.computerControlModel || ""} onChange={(event) => void apply(() => app.SetComputerControlModel(event.target.value))}><option value="">{t("settings.computer.autoSelect")}</option>{modelOptions.map((model) => <option key={model} value={model}>{model}</option>)}</select>
      </SettingsField>
      <SettingsField label={t("settings.computer.fullAccess")} hint={t("settings.computer.fullAccessHint")}>
        <button className={`btn ${s.computerUseFullAccessApproved ? "btn--primary" : "btn--ghost"}`} disabled={busy || !state?.capabilities.supported} onClick={() => void apply(() => app.SetComputerUseFullAccess(!s.computerUseFullAccessApproved))}>{s.computerUseFullAccessApproved ? <><ShieldCheck size={14} />{t("settings.computer.authorized")}</> : <><ShieldCheck size={14} />{t("settings.computer.authorize")}</>}</button>
      </SettingsField>
      {state && <div className="settings-field__hint-line">{state.capabilities.supported ? t("settings.computer.capabilities", { details: [state.capabilities.uiAutomation ? t("settings.computer.uiAutomation") : t("settings.computer.noUiAutomation"), state.capabilities.screenCapture ? t("settings.computer.screenCapture") : t("settings.computer.noScreenCapture"), state.capabilities.overlay ? t("settings.computer.overlay") : t("settings.computer.noOverlay")].join(", ") }) : t("settings.computer.unavailable", { reason: state.capabilities.unavailableReason || t("settings.computer.platformUnsupported") })}</div>}
    </SettingsSection>
  </>;
}

function AboutSection() {
  const t = useT();
  const [version, setVersion] = useState("-");
  useEffect(() => { void app.Version().then(setVersion).catch(() => {}); }, []);
  return <SettingsSection title={t("settings.about.title")} description={t("settings.about.description")}>
    <div className="settings-about"><strong>{t("settings.about.version", { version })}</strong><span>{t("settings.about.modes")}</span><button className="btn btn--ghost" onClick={() => openExternal("https://github.com/nanbo0ne/O.R.C.A-for-Windows")}>{t("settings.about.github")}</button></div>
  </SettingsSection>;
}

function normalizeInitialSettingsTab(tab?: SettingsTab): SettingsTab {
  if (tab === "providers") return "models";
  return tab ?? "general";
}

function settingsTabLabel(id: SettingsTab, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "general":
      return t("settings.tab.general");
    case "models":
      return t("settings.tab.models");
    case "providers":
      return t("settings.tab.providers");
    case "bots":
      return t("settings.tab.bots");
    case "mcp":
      return t("settings.tab.mcp");
    case "skills":
      return t("settings.tab.skills");
    case "memory":
      return t("settings.tab.memory");
    case "network":
      return t("settings.tab.network");
    case "permissions":
      return t("settings.tab.permissions");
    case "sandbox":
      return t("settings.tab.sandbox");
		case "appearance":
			return t("settings.tab.appearance");
		case "localAI":
			return t("settings.tab.localAI");
		case "computer":
			return t("settings.tab.computer");
		case "about":
			return t("settings.tab.about");
  }
}

function settingsTabMeta(id: SettingsTab, s: SettingsView, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "models":
      return settingsModelMeta(s, t);
    case "general":
      return `${closeBehaviorLabel(normalizeCloseBehavior(s.closeBehavior), t)} · ${t(`settings.autoPlan.${normalizeAutoPlan(s.autoPlan)}`)}`;
    case "providers":
      return t("settings.providerCount", { n: s.providers.length });
    case "bots":
      return botSettingsMeta(s.bot, t);
    case "mcp":
      return t("caps.connectorsTab");
    case "skills":
      return t("caps.skillsTab");
    case "memory":
      return t("settings.tabSub.memory");
    case "network":
      return proxyModeLabel(normalizeProxyMode(s.network.proxyMode), t);
    case "permissions":
      return permissionModeLabel(s.permissions.mode, t);
    case "sandbox":
      return sandboxModeLabel(s.sandbox.bash, t);
		case "appearance":
			return t("settings.appearanceMeta");
		case "localAI":
			return "llama.cpp";
		case "computer":
			return s.computerUseFullAccessApproved ? t("settings.computer.authorized") : t("settings.computer.authorize");
		case "about":
			return "O.R.C.A.";
  }
}

function settingsModelMeta(s: SettingsView, t: ReturnType<typeof useT>): string {
  const ref = toRef(s.defaultModel, s);
  if (!ref) return t("common.none");
  if (!ref.includes("/")) return ref;
  const [provider, ...modelParts] = ref.split("/");
  const model = modelParts.join("/") || ref;
  const providerView = s.providers.find((p) => p.name === provider);
  return `${modelProviderLabel(provider, providerView, t)} · ${model}`;
}

function botSettingsMeta(bot: BotSettingsView, t: ReturnType<typeof useT>): string {
  if (!bot.enabled) return t("settings.botMetaOff");
  const channels = [bot.qq.enabled, bot.feishu.enabled, bot.weixin.enabled].filter(Boolean).length;
  if (channels === 0) return t("settings.botMetaNoChannels");
  return t("settings.botMetaChannels", { n: channels });
}

// allRefs flattens providers into "provider/model" refs for the model selectors.
function allRefs(s: SettingsView): string[] {
  const out: string[] = [];
  for (const p of s.providers) {
    if (!p.added || !p.keySet) continue;
    for (const m of p.models) out.push(`${p.name}/${m}`);
  }
  return out;
}

// toRef normalises a stored model id (a provider name, a bare model, or a ref) to
// a "provider/model" ref so a <select> of refs can show it selected.
function toRef(model: string, s: SettingsView): string {
  if (!model) return "";
  if (model.includes("/")) return model;
  const byName = s.providers.find((p) => p.name === model);
  if (byName) return `${byName.name}/${byName.default || byName.models[0] || ""}`;
  const byModel = s.providers.find((p) => p.models.includes(model));
  if (byModel) return `${byModel.name}/${model}`;
  return model;
}

const PROXY_MODES = ["auto", "custom", "off"] as const;

// EFFORT_PRESETS is the canonical union of /effort levels the kernel
// recognises. The settings UI exposes these as toggleable checkboxes; users
// can additionally add arbitrary custom names via the "Add" input. The order
// here is what the user sees in the dropdown.
const EFFORT_PRESETS: readonly string[] = ["low", "medium", "high", "xhigh", "max"];
const REASONING_PROTOCOLS: readonly string[] = ["", "deepseek", "openai", "none"];
const PROXY_TYPES = ["http", "https", "socks5", "socks5h"] as const;
const LANGUAGE_PREFS: LangPref[] = ["", "zh", "en"];
const AUTO_PLAN_MODES = ["off", "on"] as const;
const ENGINEERING_PROMPT_MODES: PromptMode[] = ["coding", "assistant"];

type ProxyMode = (typeof PROXY_MODES)[number];
type AutoPlanMode = (typeof AUTO_PLAN_MODES)[number];

function normalizeProxyMode(mode: string): ProxyMode {
  switch (mode) {
    case "custom":
      return "custom";
    case "off":
      return "off";
    default:
      return "auto";
  }
}

function normalizeNetworkView(network: NetworkView): NetworkView {
  return { ...network, proxyMode: normalizeProxyMode(network.proxyMode) };
}

function normalizeAutoPlan(mode: string | undefined): AutoPlanMode {
  return mode === "ask" || mode === "on" ? "on" : "off";
}

function normalizeReasoningProtocol(protocol: string | undefined): string {
  return REASONING_PROTOCOLS.includes(protocol ?? "") ? protocol ?? "" : "";
}

function defaultBotSettings(): BotSettingsView {
  return {
    enabled: true,
    model: "",
    promptMode: "orca",
    workspaceRoot: "",
    maxSteps: 0,
    debounceMs: 1500,
    allowlist: {
      enabled: true,
      allowAll: true,
      qqUsers: [],
      feishuUsers: [],
      weixinUsers: [],
      qqGroups: [],
      feishuGroups: [],
      weixinGroups: [],
    },
    qq: { enabled: false, appId: "", appSecretEnv: "QQ_BOT_APP_SECRET", secretSet: false, environment: "production" },
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
      enabled: true,
      accountId: "default",
      tokenEnv: "WEIXIN_BOT_TOKEN",
      tokenSet: false,
      apiBase: "https://ilinkai.weixin.qq.com",
    },
    connections: [],
  };
}

function normalizeBotSettings(bot: BotSettingsView | null | undefined, _promptModes: PromptMode[] = ENGINEERING_PROMPT_MODES): BotSettingsView {
  const fallback = defaultBotSettings();
  const allowlist = bot?.allowlist ?? fallback.allowlist;
  const mode = bot?.feishu?.mode === "websocket" ? "websocket" : "webhook";
  return {
    ...fallback,
    ...bot,
    promptMode: "orca",
    workspaceRoot: String(bot?.workspaceRoot ?? fallback.workspaceRoot).trim(),
    maxSteps: Math.max(0, Number(bot?.maxSteps ?? fallback.maxSteps) || 0),
    debounceMs: Number(bot?.debounceMs) || fallback.debounceMs,
    allowlist: {
      ...fallback.allowlist,
      ...allowlist,
      qqUsers: asArray(allowlist.qqUsers),
      feishuUsers: asArray(allowlist.feishuUsers),
      weixinUsers: asArray(allowlist.weixinUsers),
      qqGroups: asArray(allowlist.qqGroups),
      feishuGroups: asArray(allowlist.feishuGroups),
      weixinGroups: asArray(allowlist.weixinGroups),
    },
    qq: { ...fallback.qq, ...bot?.qq, environment: bot?.qq?.environment === "sandbox" ? "sandbox" : "production" },
    feishu: { ...fallback.feishu, ...bot?.feishu, enabled: false, domain: bot?.feishu?.domain === "lark" ? "lark" : "feishu", mode },
    weixin: { ...fallback.weixin, ...bot?.weixin, enabled: true },
    connections: asArray(bot?.connections).map(normalizeBotConnection),
  };
}

function normalizeBotConnection(raw: any) {
  const credential = raw?.credential ?? {};
  return {
    id: String(raw?.id ?? "").trim(),
    provider: String(raw?.provider ?? "").trim(),
    domain: String(raw?.domain ?? "").trim(),
    label: String(raw?.label ?? "").trim(),
    enabled: raw?.enabled !== false,
    status: String(raw?.status ?? "disconnected").trim(),
    credential: {
      appId: String(credential.appId ?? "").trim(),
      appSecretEnv: String(credential.appSecretEnv ?? "").trim(),
      accountId: String(credential.accountId ?? "").trim(),
      tokenEnv: String(credential.tokenEnv ?? "").trim(),
      environment: String(credential.environment ?? "").trim(),
      secretSet: Boolean(credential.secretSet),
    },
    sessionMappings: asArray(raw?.sessionMappings).map((item: any) => ({
      remoteId: String(item?.remoteId ?? "").trim(),
      sessionId: String(item?.sessionId ?? "").trim(),
      updatedAt: String(item?.updatedAt ?? "").trim(),
    })),
    guideSent: Boolean(raw?.guideSent),
    lastError: String(raw?.lastError ?? "").trim(),
    createdAt: String(raw?.createdAt ?? "").trim(),
    updatedAt: String(raw?.updatedAt ?? "").trim(),
  };
}

function normalizeSettingsView(view: SettingsView | null | undefined): SettingsView | null {
  if (!view) return null;
  const permissions = view.permissions ?? { mode: "ask", autoReviewModel: "", allow: [], ask: [], deny: [] };
  const sandbox = view.sandbox ?? { bash: "enforce", network: false, workspaceRoot: "", allowWrite: [] };
  const network = view.network ?? {
    proxyMode: "auto",
    proxyUrl: "",
    noProxy: "",
    proxy: { type: "socks5", server: "", port: 0, username: "", password: "" },
  };
  const agent = view.agent ?? { temperature: 0, maxSteps: 0, plannerMaxSteps: 12, systemPrompt: "" };
  agent.plannerMaxSteps = Number.isFinite(agent.plannerMaxSteps) ? Math.max(0, Math.trunc(agent.plannerMaxSteps)) : 12;
  agent.maxSteps = Number.isFinite(agent.maxSteps) ? Math.max(0, Math.trunc(agent.maxSteps)) : 0;
  return {
    ...view,
    uiScale: 0,
    effectiveUIScale: 100,
    providers: asArray(view.providers).map((p) => ({
      ...p,
      builtIn: Boolean(p.builtIn),
      added: Boolean(p.added),
      models: asArray(p.models),
      modelsUrl: p.modelsUrl ?? "",
      reasoningProtocol: normalizeReasoningProtocol(p.reasoningProtocol),
      supportedEfforts: asArray(p.supportedEfforts),
    })),
    providerKinds: asArray(view.providerKinds),
    permissions: {
      ...permissions,
      autoReviewModel: String(permissions.autoReviewModel ?? "").trim(),
      allow: asArray(permissions.allow),
      ask: asArray(permissions.ask),
      deny: asArray(permissions.deny),
    },
    sandbox: {
      ...sandbox,
      allowWrite: asArray(sandbox.allowWrite),
    },
    network: {
      ...network,
      proxy: network.proxy ?? { type: "socks5", server: "", port: 0, username: "", password: "" },
    },
    agent,
    bot: normalizeBotSettings(view.bot),
    autoPlan: normalizeAutoPlan(view.autoPlan),
    autoApproveTools: Boolean(view.autoApproveTools ?? view.bypass),
    bypass: Boolean(view.autoApproveTools ?? view.bypass),
    desktopLanguage: normalizeLangPref(view.desktopLanguage),
    desktopTheme: "light",
    desktopThemeStyle: "slate",
    desktopUIStyle: view.desktopUIStyle === "classic" ? "classic" : "modern",
    closeBehavior: normalizeCloseBehavior(view.closeBehavior),
    checkUpdates: view.checkUpdates !== false,
    processDisplayMode: view.processDisplayMode === "compact" || view.processDisplayMode === "detailed"
      ? view.processDisplayMode
      : view.expandThinking ? "detailed" : "compact",
    activityIndicatorEnabled: Boolean(view.activityIndicatorEnabled),
    visionEnabled: Boolean(view.visionEnabled),
    visionMode: view.visionMode === "off" || view.visionMode === "on"
      ? view.visionMode
      : view.visionEnabled ? "on" : "auto",
  };
}

type CloseBehavior = "background" | "quit";

function normalizeCloseBehavior(mode: string | undefined): CloseBehavior {
  return mode === "quit" ? "quit" : "background";
}

function closeBehaviorLabel(mode: CloseBehavior, t: ReturnType<typeof useT>): string {
  return mode === "quit" ? t("settings.closeBehavior.quit") : t("settings.closeBehavior.background");
}

function permissionModeLabel(mode: string, t: ReturnType<typeof useT>): string {
  switch (mode) {
    case "allow":
      return t("settings.modeAllowShort");
    case "deny":
      return t("settings.modeDenyShort");
    default:
      return t("settings.modeAskShort");
  }
}

function sandboxModeLabel(mode: string, t: ReturnType<typeof useT>): string {
  return mode === "off" ? t("settings.bashOffShort") : t("settings.bashEnforceShort");
}

function reasoningProtocolLabel(protocol: string, t: ReturnType<typeof useT>): string {
  switch (protocol) {
    case "deepseek":
      return t("settings.reasoningProtocol.deepseek");
    case "openai":
      return t("settings.reasoningProtocol.openai");
    case "none":
      return t("settings.reasoningProtocol.none");
    default:
      return t("settings.reasoningProtocol.auto");
  }
}

function GeneralSection({ s, busy, apply }: SectionProps) {
  const { t, setPref } = useI18n();
  const [updateStatus, setUpdateStatus] = useState<string | null>(null);
  const [currentVersion, setCurrentVersion] = useState("");
  useEffect(() => {
    void app.Version().then(setCurrentVersion).catch(() => setCurrentVersion(""));
  }, []);
  const closeBehavior = normalizeCloseBehavior(s.closeBehavior);
  const autoPlan = normalizeAutoPlan(s.autoPlan);
  const languagePref = normalizeLangPref(s.desktopLanguage);
  const setLanguage = (next: LangPref) => {
    setPref(next);
    void apply(() => app.SetDesktopLanguage(next));
  };
  const checkNow = async () => {
    setUpdateStatus(t("settings.updateChecking"));
    try {
      const currentVersion = await app.Version();
      const update = await checkDesktopUpdate(currentVersion, true);
      if (update?.err) throw new Error(update.err);
      setUpdateStatus(update?.available ? t("settings.updateAvailable", { version: update.latest }) : t("settings.updateCurrent"));
    } catch {
      setUpdateStatus(t("settings.updateFailed"));
    }
  };
  return (
    <SettingsSection title={t("settings.tab.general")}>
      <SettingsField label={t("settings.language")}>
        <div className="set-seg">
          {LANGUAGE_PREFS.map((pref) => (
            <button
              key={pref || "auto"}
              className={`set-seg__btn${languagePref === pref ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => setLanguage(pref)}
            >
              {pref === "" ? t("settings.langAuto") : pref === "zh" ? "中文" : "English"}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.closeBehavior")} hint={t("settings.closeBehaviorHint")}>
        <div className="set-seg">
          {(["background", "quit"] as const).map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${closeBehavior === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetCloseBehavior(mode))}
            >
              {closeBehaviorLabel(mode, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.autoCheckUpdates")} hint={t("settings.autoCheckUpdatesHint")}>
        <div className="settings-update-control">
          <ToggleSegment
            value={s.checkUpdates}
            disabled={busy}
            onChange={(enabled) => void apply(() => app.SetDesktopCheckUpdates(enabled))}
          />
          <button type="button" className="btn btn--small" disabled={busy} onClick={() => void checkNow()}>
            {t("settings.checkUpdatesNow")}
          </button>
          {updateStatus && <span className="settings-update-control__status" role="status">{updateStatus}</span>}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.currentVersion")}>
        <span className="settings-update-control__status">{currentVersion || "-"}</span>
      </SettingsField>
      <SettingsField label={t("settings.processDisplay")} hint={t("settings.processDisplayHint")}>
        <div className="set-seg">
          {(["compact", "detailed"] as ProcessDisplayMode[]).map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${s.processDisplayMode === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetProcessDisplayMode(mode))}
            >
              {t(`settings.processDisplay.${mode}`)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.activityIndicator")} hint={t("settings.activityIndicatorHint")}>
        <ToggleSegment
          value={s.activityIndicatorEnabled}
          disabled={busy}
          onChange={(enabled) => void apply(() => app.SetActivityIndicatorEnabled(enabled))}
        />
      </SettingsField>
      <SettingsField label={t("settings.autoPlan")}>
        <div className="set-seg">
          {AUTO_PLAN_MODES.map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${autoPlan === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => void apply(() => app.SetAutoPlan(mode))}
            >
              {t(`settings.autoPlan.${mode}`)}
            </button>
          ))}
        </div>
      </SettingsField>
    </SettingsSection>
  );
}

function StepLimitControl({
  value,
  presets,
  busy,
  onChange,
}: {
  value: number;
  presets: number[];
  busy: boolean;
  onChange: (value: number) => void;
}) {
  const t = useT();
  const normalized = normalizeStepLimit(value);
  const presetSet = new Set(presets.map(normalizeStepLimit));
  const [custom, setCustom] = useState(String(normalized));
  useEffect(() => setCustom(String(normalized)), [normalized]);
  const isCustom = !presetSet.has(normalized);
  const commitCustom = () => {
    const next = normalizeStepLimit(Number(custom));
    setCustom(String(next));
    if (next !== normalized) onChange(next);
  };
  return (
    <div className="step-limit-control">
      <div className="set-seg">
        {presets.map((preset) => {
          const n = normalizeStepLimit(preset);
          return (
            <button
              key={n}
              type="button"
              className={`set-seg__btn${normalized === n ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => n !== normalized && onChange(n)}
            >
              {stepLimitLabel(n, t)}
            </button>
          );
        })}
        <button
          type="button"
          className={`set-seg__btn${isCustom ? " set-seg__btn--on" : ""}`}
          disabled={busy}
          onClick={() => {
            if (!isCustom) setCustom(String(normalized || 12));
          }}
        >
          {t("settings.stepLimit.custom")}
        </button>
      </div>
      <input
        className="mem-input step-limit-control__custom"
        value={custom}
        disabled={busy}
        inputMode="numeric"
        aria-label={t("settings.stepLimit.custom")}
        onChange={(e) => setCustom(e.target.value.replace(/[^\d]/g, ""))}
        onBlur={commitCustom}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
        }}
      />
    </div>
  );
}

function normalizeStepLimit(value: number): number {
  return Number.isFinite(value) && value > 0 ? Math.trunc(value) : 0;
}

function stepLimitLabel(value: number, t: ReturnType<typeof useT>): string {
  return value === 0 ? t("settings.stepLimit.unlimited") : String(value);
}

function CompactRatioControl({
  value,
  busy,
  onChange,
}: {
  value: number;
  busy: boolean;
  onChange: (value: number) => void;
}) {
  const normalized = Math.round(clampNumber(value || 0.8, 0.2, 0.95) * 100);
  const [draft, setDraft] = useState(normalized);
  useEffect(() => setDraft(normalized), [normalized]);
  const commit = (pct: number) => {
    const next = Math.round(clampNumber(pct, 20, 95));
    setDraft(next);
    if (next !== normalized) onChange(next / 100);
  };
  return (
    <div className="compact-ratio-control">
      <input
        className="compact-ratio-control__range"
        type="range"
        min={20}
        max={95}
        step={5}
        value={draft}
        disabled={busy}
        onChange={(e) => setDraft(Number(e.target.value))}
        onBlur={(e) => commit(Number(e.currentTarget.value))}
        onMouseUp={(e) => commit(Number((e.currentTarget as HTMLInputElement).value))}
        onTouchEnd={(e) => commit(Number((e.currentTarget as HTMLInputElement).value))}
        onKeyUp={(e) => {
          if (e.key === "ArrowLeft" || e.key === "ArrowRight" || e.key === "Home" || e.key === "End") {
            commit(Number((e.currentTarget as HTMLInputElement).value));
          }
        }}
      />
      <span className="compact-ratio-control__value">{draft}%</span>
    </div>
  );
}

function NetworkSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const savedNetwork = normalizeNetworkView(s.network);
  const [draft, setDraft] = useState<NetworkView>(savedNetwork);
  useEffect(() => setDraft(normalizeNetworkView(s.network)), [s.network]);
  const dirty = JSON.stringify(draft) !== JSON.stringify(savedNetwork);
  const setProxy = (next: Partial<NetworkView["proxy"]>) => {
    setDraft({ ...draft, proxy: { ...draft.proxy, ...next } });
  };

  return (
    <SettingsSection
      title={t("settings.tab.network")}
      actions={
        <button
          className="btn btn--primary btn--small"
          disabled={busy || !dirty}
          onClick={() => void apply(() => app.SetNetwork(draft))}
        >
          {t("settings.saveNetwork")}
        </button>
      }
    >
      <SettingsField label={t("settings.proxyMode")}>
        <div className="set-seg">
          {PROXY_MODES.map((mode) => (
            <button
              key={mode}
              className={`set-seg__btn${draft.proxyMode === mode ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => setDraft({ ...draft, proxyMode: mode })}
            >
              {proxyModeLabel(mode, t)}
            </button>
          ))}
        </div>
      </SettingsField>

      {draft.proxyMode === "custom" && (
        <>
          <SettingsField label={t("settings.proxyType")}>
            <div className="set-seg">
              {PROXY_TYPES.map((typ) => (
                <button
                  key={typ}
                  className={`set-seg__btn${draft.proxy.type === typ ? " set-seg__btn--on" : ""}`}
                  disabled={busy}
                  onClick={() => setProxy({ type: typ })}
                >
                  {typ.toUpperCase()}
                </button>
              ))}
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyServer")}>
            <div className="settings-inline-controls">
            <input
              className="mem-input set-grow"
              placeholder="127.0.0.1"
              value={draft.proxy.server}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ server: e.target.value })}
            />
            <label className="set-label">{t("settings.proxyPort")}</label>
            <input
              className="mem-input set-narrow"
              placeholder="7890"
              value={draft.proxy.port ? String(draft.proxy.port) : ""}
              disabled={busy || !!draft.proxyUrl.trim()}
              inputMode="numeric"
              onChange={(e) => setProxy({ port: Number(e.target.value) || 0 })}
            />
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyUsername")}>
            <div className="settings-inline-controls">
            <input
              className="mem-input set-grow"
              value={draft.proxy.username}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ username: e.target.value })}
            />
            <label className="set-label">{t("settings.proxyPassword")}</label>
            <input
              className="mem-input set-grow"
              type="password"
              value={draft.proxy.password}
              disabled={busy || !!draft.proxyUrl.trim()}
              onChange={(e) => setProxy({ password: e.target.value })}
            />
            </div>
          </SettingsField>
          <SettingsField label={t("settings.proxyUrl")} hint={t("settings.proxyUrlHint")}>
              <input
                className="mem-input set-grow"
                placeholder="socks5://127.0.0.1:7890"
                value={draft.proxyUrl}
                disabled={busy}
                onChange={(e) => setDraft({ ...draft, proxyUrl: e.target.value })}
              />
          </SettingsField>
          <SettingsField label={t("settings.noProxy")}>
            <input
              className="mem-input set-grow"
              placeholder="localhost,127.0.0.1,.local"
              value={draft.noProxy}
              disabled={busy}
              onChange={(e) => setDraft({ ...draft, noProxy: e.target.value })}
            />
          </SettingsField>
        </>
      )}
    </SettingsSection>
  );
}

type BotChannelID = "qq" | "weixin";
type BotInstallTarget = "qq" | "feishu" | "lark" | "weixin";
type BotInstallState = {
  target: BotInstallTarget | "";
  result: BotInstallStartResult | null;
  status: "idle" | "starting" | "showing" | "manual" | "connected" | "error";
  message: string;
};
const BOT_INSTALL_TARGETS: BotInstallTarget[] = ["qq", "weixin", "feishu", "lark"];

function BotsSection({ s, busy, apply, promptModes }: SectionProps & { promptModes: PromptMode[] }) {
  const t = useT();
  const savedBot = normalizeBotSettings(s.bot, promptModes);
  const [draft, setDraft] = useState<BotSettingsView>(savedBot);
  const [secrets, setSecrets] = useState<Record<BotChannelID, string>>({ qq: "", weixin: "" });
  const [install, setInstall] = useState<BotInstallState>({ target: "", result: null, status: "idle", message: "" });
  const [diagnostics, setDiagnostics] = useState<Record<string, string>>({});
  const [testTargets, setTestTargets] = useState<Record<string, string>>({});
  const [runtime, setRuntime] = useState<{ status: string; message: string; channels: string[] } | null>(null);

  useEffect(() => {
    setDraft(normalizeBotSettings(s.bot, promptModes));
    setSecrets({ qq: "", weixin: "" });
    setTestTargets({});
  }, [promptModes, s.bot]);

  useEffect(() => {
    let cancelled = false;
    void app.GetBotRuntimeStatus?.().then((next) => {
      if (!cancelled) setRuntime(next);
    }).catch(() => {
      if (!cancelled) setRuntime(null);
    });
    return () => { cancelled = true; };
  }, [draft.connections.length, install.status]);

  const dirty = JSON.stringify(sanitizeBotDraft(draft, promptModes)) !== JSON.stringify(sanitizeBotDraft(savedBot, promptModes));
  const setQQ = (next: Partial<BotSettingsView["qq"]>) =>
    setDraft((prev) => ({ ...prev, qq: { ...prev.qq, ...next } }));
  const setWeixin = (next: Partial<BotSettingsView["weixin"]>) =>
    setDraft((prev) => ({ ...prev, weixin: { ...prev.weixin, ...next } }));
  const setConnections = (mapper: (connections: BotConnectionView[]) => BotConnectionView[]) =>
    setDraft((prev) => ({ ...prev, connections: mapper(prev.connections) }));
  const installStep = install.status === "connected" ? 3 : install.status === "starting" || install.status === "showing" ? 2 : 1;

  const saveBot = () => app.SetBotSettings(sanitizeBotDraft(draft, promptModes));
  const startInstall = async (target: BotInstallTarget) => {
    if (target === "feishu" || target === "lark") {
      setInstall({ target, result: null, status: "error", message: t("settings.botUnavailable") });
      return;
    }
    if (target === "qq") {
      setInstall({ target, result: null, status: "manual", message: t("settings.botQQManualHint") });
      return;
    }
    setInstall({ target, result: null, status: "starting", message: "" });
    const provider = "weixin";
    const domain = "weixin";
    const result = await app.StartBotConnectionInstall(provider, domain);
    if (!result.ok) {
      setInstall({ target, result, status: "error", message: result.message || t("settings.botInstallFailed") });
      return;
    }
    setInstall({ target, result, status: "showing", message: result.message || t("settings.botInstallScanHint") });
  };
  const saveQQConnection = async (connect: boolean) => {
    const appId = draft.qq.appId.trim();
    const appSecret = secrets.qq.trim();
    const environment = draft.qq.environment === "production" ? "production" : "sandbox";
    await apply(async () => {
      if (connect) {
        const connection = await app.ConnectQQBot({ appId, appSecret, environment });
        setDraft((prev) => ({
          ...prev,
          enabled: true,
          qq: { ...prev.qq, enabled: true, appId, appSecretEnv: "QQ_BOT_APP_SECRET", secretSet: true, environment },
          connections: [...prev.connections.filter((c) => c.id !== connection.id), connection],
        }));
        setInstall({
          target: "qq",
          result: null,
          status: connection.status === "warning" ? "manual" : "connected",
          message: connection.lastError || t("settings.botQQConnected"),
        });
      } else {
        await saveBot();
        if (appSecret) await app.SetBotSecret(draft.qq.appSecretEnv.trim() || "QQ_BOT_APP_SECRET", appSecret);
        setDraft((prev) => ({ ...prev, qq: { ...prev.qq, secretSet: prev.qq.secretSet || Boolean(appSecret), environment } }));
        setInstall((prev) => ({ ...prev, target: "qq", status: "manual", message: t("settings.botQQSaved") }));
      }
    });
    setSecrets((prev) => ({ ...prev, qq: "" }));
  };
  const pollInstall = async () => {
    if (!install.result?.installId || !install.target) return;
    const poll = await app.PollBotConnectionInstall(install.result.installId);
    if (poll.done) {
      setDraft((prev) => ({
        ...prev,
        enabled: true,
        connections: [...prev.connections.filter((c) => c.id !== poll.connection.id), poll.connection],
      }));
      setInstall({ target: install.target, result: install.result, status: "connected", message: poll.message || t("settings.botInstallConnected") });
      return;
    }
    if (poll.error) {
      setInstall((prev) => ({ ...prev, status: "error", message: poll.error }));
      return;
    }
    setInstall((prev) => ({ ...prev, message: poll.message || t("settings.botInstallWaiting") }));
  };
  useEffect(() => {
    if (install.status !== "showing" || !install.result?.installId) return;
    const interval = window.setInterval(() => void pollInstall(), Math.max(install.result.interval || 3, 3) * 1000);
    return () => window.clearInterval(interval);
  }, [install.status, install.result?.installId]);
  const diagnoseConnection = async (id: string) => {
    const diag = await app.DiagnoseBotConnection(id);
    setDiagnostics((prev) => ({ ...prev, [id]: diag.message || diag.status }));
  };
  const testConnection = async (connection: BotConnectionView) => {
    const target = (testTargets[connection.id] ?? firstConnectionRemote(connection)).trim();
    const diag = await app.TestBotConnection(connection.id, target);
    setDiagnostics((prev) => ({ ...prev, [connection.id]: diag.message || diag.status }));
    if (diag.messageId && target) {
      const updatedAt = new Date().toISOString();
      setConnections((items) => items.map((item) => {
        if (item.id !== connection.id) return item;
        const sessionMappings = [
          ...item.sessionMappings.filter((mapping) => mapping.remoteId !== target),
          { remoteId: target, sessionId: "", updatedAt },
        ];
        return { ...item, sessionMappings, updatedAt };
      }));
    }
  };
  const saveSecret = async (channel: BotChannelID, envName: string) => {
    const env = envName.trim();
    const value = secrets[channel].trim();
    if (!env || !value) return;
    await apply(async () => {
      await saveBot();
      await app.SetBotSecret(env, value);
    });
    setSecrets((prev) => ({ ...prev, [channel]: "" }));
  };
  const clearSecret = async (envName: string) => {
    const env = envName.trim();
    if (!env) return;
    await apply(async () => {
      await saveBot();
      await app.ClearBotSecret(env);
    });
  };

  return (
    <>
      <SettingsSection
        title={t("settings.botSetup")}
        description={t("settings.botSetupHint")}
        actions={
          <button
            className="btn btn--primary btn--small"
            disabled={busy || !dirty}
            onClick={() => void apply(saveBot)}
          >
            {t("settings.saveBotSettings")}
          </button>
        }
      >
        <div className={`bot-runtime-status bot-runtime-status--${runtime?.status ?? "idle"}`}>
          <strong>{runtime?.message || t("settings.botRuntimeIdle")}</strong>
          <span>{t("settings.botRuntimeAuto")}</span>
        </div>
        <details className="bot-advanced">
          <summary>{t("settings.botAdvancedRuntime")}</summary>
          <div className="bot-advanced__body">
            <SettingsField label={t("settings.botRuntime")}>
              <div className="settings-inline-controls">
                <label className="set-label">{t("settings.botMaxSteps")}</label>
                <input
                  className="mem-input set-narrow"
                  value={draft.maxSteps ? String(draft.maxSteps) : ""}
                  placeholder="25"
                  disabled={busy}
                  inputMode="numeric"
                  onChange={(e) => setDraft((prev) => ({ ...prev, maxSteps: parseNonNegativeInt(e.target.value) }))}
                />
                <label className="set-label">{t("settings.botDebounceMs")}</label>
                <input
                  className="mem-input set-narrow"
                  value={draft.debounceMs ? String(draft.debounceMs) : ""}
                  placeholder="1500"
                  disabled={busy}
                  inputMode="numeric"
                  onChange={(e) => setDraft((prev) => ({ ...prev, debounceMs: parseNonNegativeInt(e.target.value) }))}
                />
              </div>
            </SettingsField>
          </div>
        </details>
      </SettingsSection>

      <SettingsSection title={t("settings.botChannels")} description={t("settings.botChannelsHint")}>
        <div className="bot-connect-layout">
          <div className="bot-connect-steps">
            {[t("settings.botInstallStepPick"), t("settings.botInstallStepScan"), t("settings.botInstallStepDone")].map((label, index) => (
              <div key={label} className={`bot-connect-step${installStep >= index + 1 ? " bot-connect-step--active" : ""}`}>
                <span>{index + 1}</span>
                <strong>{label}</strong>
              </div>
            ))}
          </div>
          <div className="bot-connect-targets">
            {BOT_INSTALL_TARGETS.map((target) => (
              <button
                key={target}
                type="button"
                className={`bot-connect-target${install.target === target ? " bot-connect-target--active" : ""}${target === "feishu" || target === "lark" ? " bot-connect-target--disabled" : ""}`}
                disabled={busy || install.status === "starting" || target === "feishu" || target === "lark"}
                onClick={() => void startInstall(target)}
              >
                <strong>{botTargetLabel(target, t)}</strong>
                <span>{botTargetHint(target, t)}</span>
              </button>
            ))}
          </div>

          <div className={`bot-connect-panel${install.target === "qq" ? " bot-connect-panel--qq" : ""}`}>
            {install.target === "qq" ? (
              <QQConnectPanel
                draft={draft}
                secret={secrets.qq}
                busy={busy}
                setQQ={setQQ}
                setSecret={(value) => setSecrets((prev) => ({ ...prev, qq: value }))}
                onApply={(connect) => void saveQQConnection(connect)}
              />
            ) : (
              <>
                <div className="bot-connect-panel__qr">
                  {install.status === "showing" && install.result?.url ? (
                    <img src={`https://api.qrserver.com/v1/create-qr-code/?size=196x196&data=${encodeURIComponent(install.result.url)}`} alt={t("settings.botInstallQrAlt")} />
                  ) : (
                    <div className="bot-connect-panel__placeholder">{install.status === "starting" ? t("settings.botInstallStarting") : t("settings.botInstallPick")}</div>
                  )}
                </div>
                <div className="bot-connect-panel__body">
                  <strong>{install.target ? botTargetLabel(install.target, t) : t("settings.botInstallTitle")}</strong>
                  <p>{install.message || t("settings.botInstallSubtitle")}</p>
                  {install.result?.userCode ? <code>{install.result.userCode}</code> : null}
                  {install.status === "showing" ? (
                    <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void pollInstall()}>
                      {t("settings.botInstallCheck")}
                    </button>
                  ) : null}
                </div>
              </>
            )}
          </div>

          <div className="bot-connection-list">
            {draft.connections.length === 0 ? (
              <div className="bot-connection-empty">{t("settings.botConnectionsEmpty")}</div>
            ) : (
              draft.connections.map((connection) => (
                <div key={connection.id} className="bot-connection-row">
                  <div>
                    <strong>{connection.label || botConnectionLabel(connection, t)}</strong>
                    <span>{botConnectionMeta(connection, t)}</span>
                    {diagnostics[connection.id] ? <em>{diagnostics[connection.id]}</em> : null}
                  </div>
	                  <div className="bot-connection-row__actions">
                    {connection.provider === "feishu" || connection.provider === "weixin" ? (
                      <input
                        className="mem-input bot-connection-row__target"
                        value={testTargets[connection.id] ?? firstConnectionRemote(connection)}
                        disabled={busy}
                        placeholder={t("settings.botTestChatId")}
                        spellCheck={false}
                        onChange={(event) => setTestTargets((prev) => ({ ...prev, [connection.id]: event.target.value }))}
                      />
                    ) : null}
	                    <ToggleSegment
                      value={connection.enabled}
                      disabled={busy}
                      onChange={(enabled) => setConnections((items) => items.map((item) => item.id === connection.id ? { ...item, enabled } : item))}
                    />
                    <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void diagnoseConnection(connection.id)}>
                      {t("settings.botDiagnose")}
                    </button>
	                    <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => void testConnection(connection)}>
	                      {t("settings.botTest")}
                    </button>
                  </div>
                </div>
              ))
            )}
          </div>

          <details className="bot-advanced">
            <summary>{t("settings.botAdvancedSettings")}</summary>
            <div className="bot-advanced__body bot-advanced__body--channels">
              <BotLegacyAdvancedFields
                draft={draft}
                busy={busy}
                secrets={secrets}
                setQQ={setQQ}
                setWeixin={setWeixin}
                setSecrets={setSecrets}
                saveSecret={saveSecret}
                clearSecret={clearSecret}
              />
            </div>
          </details>
        </div>
      </SettingsSection>

    </>
  );
}

function botTargetLabel(target: BotInstallTarget, t: ReturnType<typeof useT>): string {
  switch (target) {
    case "qq": return "QQ";
    case "lark": return "Lark";
    case "weixin": return t("settings.botWeixin");
    default: return t("settings.botFeishu");
  }
}

function botTargetHint(target: BotInstallTarget, t: ReturnType<typeof useT>): string {
  switch (target) {
    case "qq": return t("settings.botInstallQQHint");
    case "lark": return t("settings.botInstallLarkHint");
    case "weixin": return t("settings.botInstallWeixinHint");
    default: return t("settings.botInstallFeishuHint");
  }
}

function botConnectionLabel(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  if (connection.domain === "lark") return "Lark";
  if (connection.provider === "weixin") return t("settings.botWeixin");
  if (connection.provider === "qq") return "QQ";
  return t("settings.botFeishu");
}

function botConnectionMeta(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  const status = connection.status === "connected" ? t("settings.botConnectionConnected") : connection.status || t("settings.botConnectionDisconnected");
  const secret = connection.provider === "weixin"
    ? (connection.credential.secretSet ? t("settings.botLoginTokenSet") : t("settings.botLoginTokenMissing"))
    : (connection.credential.secretSet ? t("settings.botSecretSet") : t("settings.botSecretMissing"));
  return `${status} · ${secret}`;
}

function firstConnectionRemote(connection: BotConnectionView): string {
  return connection.sessionMappings.find((mapping) => mapping.remoteId.trim())?.remoteId ?? "";
}

function QQConnectPanel({
  draft,
  secret,
  busy,
  setQQ,
  setSecret,
  onApply,
}: {
  draft: BotSettingsView;
  secret: string;
  busy: boolean;
  setQQ: (next: Partial<BotSettingsView["qq"]>) => void;
  setSecret: (value: string) => void;
  onApply: (connect: boolean) => void;
}) {
  const t = useT();
  const env = draft.qq.environment === "production" ? "production" : "sandbox";
  const canSave = Boolean(draft.qq.appId.trim() && (secret.trim() || draft.qq.secretSet));
  return (
    <div className="bot-qq-connect">
      <div className="bot-qq-connect__head">
        <div>
          <strong>{t("settings.botQQConfigureTitle")}</strong>
          <p>{t("settings.botQQConfigureHint")}</p>
        </div>
      </div>
      <div className="bot-qq-connect__rows">
        <label className="bot-qq-connect__row">
          <span>{t("settings.botAppId")}</span>
          <input className="mem-input" value={draft.qq.appId} disabled={busy} placeholder="1020..." spellCheck={false} onChange={(e) => setQQ({ appId: e.target.value })} />
        </label>
        <label className="bot-qq-connect__row">
          <span>{t("settings.botAppSecret")}</span>
          <input className="mem-input" value={secret} type="password" disabled={busy} placeholder={draft.qq.secretSet ? t("settings.botSecretReplace") : t("settings.botSecretPaste")} spellCheck={false} onChange={(e) => setSecret(e.target.value)} />
        </label>
        <div className="bot-qq-connect__row">
          <span>{t("settings.botQQEnvironment")}</span>
          <div className="set-seg">
            <button type="button" className={`set-seg__btn${env === "sandbox" ? " set-seg__btn--on" : ""}`} disabled={busy} onClick={() => setQQ({ environment: "sandbox" })}>
              {t("settings.botQQEnvSandbox")}
            </button>
            <button type="button" className={`set-seg__btn${env === "production" ? " set-seg__btn--on" : ""}`} disabled={busy} onClick={() => setQQ({ environment: "production" })}>
              {t("settings.botQQEnvProduction")}
            </button>
          </div>
        </div>
        <div className="bot-qq-connect__row">
          <span>{t("settings.botQQApply")}</span>
          <button type="button" className="btn btn--secondary btn--small" disabled={busy} onClick={() => openExternal("https://q.qq.com/qqbot/#/developer/developer-setting")}>
            {t("settings.botQQApplyButton")}
          </button>
        </div>
      </div>
      <div className="bot-qq-connect__actions">
        <button type="button" className="btn btn--secondary btn--small" disabled={busy || !canSave} onClick={() => onApply(false)}>
          {t("settings.botQQSave")}
        </button>
        <button type="button" className="btn btn--primary btn--small" disabled={busy || !canSave} onClick={() => onApply(true)}>
          {t("settings.botQQSaveConnect")}
        </button>
      </div>
    </div>
  );
}

function BotLegacyAdvancedFields({
  draft,
  busy,
  secrets,
  setQQ,
  setWeixin,
  setSecrets,
  saveSecret,
  clearSecret,
}: {
  draft: BotSettingsView;
  busy: boolean;
  secrets: Record<BotChannelID, string>;
  setQQ: (next: Partial<BotSettingsView["qq"]>) => void;
  setWeixin: (next: Partial<BotSettingsView["weixin"]>) => void;
  setSecrets: Dispatch<SetStateAction<Record<BotChannelID, string>>>;
  saveSecret: (channel: BotChannelID, envName: string) => Promise<void>;
  clearSecret: (envName: string) => Promise<void>;
}) {
  const t = useT();
  return (
    <div className="bot-legacy-grid">
      <BotChannelCard
        title="QQ"
        description={t("settings.botQQHint")}
        enabled={draft.qq.enabled}
        secretSet={draft.qq.secretSet}
        busy={busy}
        onEnabled={(enabled) => setQQ({ enabled })}
        advanced={
          <BotCardField label={t("settings.botSecretEnv")}>
            <input className="mem-input" value={draft.qq.appSecretEnv} disabled={busy} placeholder="QQ_BOT_APP_SECRET" spellCheck={false} onChange={(e) => setQQ({ appSecretEnv: e.target.value })} />
          </BotCardField>
        }
      >
        <BotCardField label={t("settings.botAppId")}>
          <input className="mem-input" value={draft.qq.appId} disabled={busy} placeholder="1020..." onChange={(e) => setQQ({ appId: e.target.value })} />
        </BotCardField>
        <BotSecretField label={t("settings.botAppSecret")} envName={draft.qq.appSecretEnv} secretSet={draft.qq.secretSet} value={secrets.qq} busy={busy} onValue={(value) => setSecrets((prev) => ({ ...prev, qq: value }))} onSave={() => void saveSecret("qq", draft.qq.appSecretEnv)} onClear={() => void clearSecret(draft.qq.appSecretEnv)} />
      </BotChannelCard>

      <BotChannelCard
        title={t("settings.botWeixin")}
        description={t("settings.botWeixinHint")}
        enabled={draft.weixin.enabled}
        secretSet={draft.weixin.tokenSet}
        secretSetLabel={draft.weixin.tokenSet ? t("settings.botLoginTokenSet") : t("settings.botLoginTokenMissing")}
        busy={busy}
        onEnabled={(enabled) => setWeixin({ enabled })}
        advanced={
          <>
            <BotCardField label={t("settings.botApiBase")}>
              <input className="mem-input" value={draft.weixin.apiBase} disabled={busy} placeholder="https://ilinkai.weixin.qq.com" onChange={(e) => setWeixin({ apiBase: e.target.value })} />
            </BotCardField>
            <BotCardField label={t("settings.botSecretEnv")}>
              <input className="mem-input" value={draft.weixin.tokenEnv} disabled={busy} placeholder="WEIXIN_BOT_TOKEN" spellCheck={false} onChange={(e) => setWeixin({ tokenEnv: e.target.value })} />
            </BotCardField>
          </>
        }
      >
        <BotCardField label={t("settings.botAccountId")}>
          <input className="mem-input" value={draft.weixin.accountId} disabled={busy} placeholder="default" onChange={(e) => setWeixin({ accountId: e.target.value })} />
        </BotCardField>
        <BotSecretField label={t("settings.botLoginToken")} envName={draft.weixin.tokenEnv} secretSet={draft.weixin.tokenSet} value={secrets.weixin} busy={busy} onValue={(value) => setSecrets((prev) => ({ ...prev, weixin: value }))} onSave={() => void saveSecret("weixin", draft.weixin.tokenEnv)} onClear={() => void clearSecret(draft.weixin.tokenEnv)} />
      </BotChannelCard>
    </div>
  );
}

function ToggleSegment({
  value,
  disabled,
  onLabel,
  offLabel,
  onChange,
}: {
  value: boolean;
  disabled: boolean;
  onLabel?: string;
  offLabel?: string;
  onChange: (value: boolean) => void;
}) {
  const t = useT();
  return (
    <div className="set-seg">
      <button
        type="button"
        className={`set-seg__btn${value ? " set-seg__btn--on" : ""}`}
        disabled={disabled}
        onClick={() => onChange(true)}
      >
        {onLabel ?? t("settings.toggleOn")}
      </button>
      <button
        type="button"
        className={`set-seg__btn${!value ? " set-seg__btn--on" : ""}`}
        disabled={disabled}
        onClick={() => onChange(false)}
      >
        {offLabel ?? t("settings.toggleOff")}
      </button>
    </div>
  );
}

function BotChannelCard({
  title,
  description,
  enabled,
  secretSet,
  secretSetLabel,
  busy,
  onEnabled,
  advanced,
  children,
}: {
  title: ReactNode;
  description: ReactNode;
  enabled: boolean;
  secretSet: boolean;
  secretSetLabel?: ReactNode;
  busy: boolean;
  onEnabled: (enabled: boolean) => void;
  advanced?: ReactNode;
  children: ReactNode;
}) {
  const t = useT();
  return (
    <article className={`bot-channel-card${enabled ? " bot-channel-card--enabled" : ""}`}>
      <div className="bot-channel-card__head">
        <div className="bot-channel-card__identity">
          <strong>{title}</strong>
          <span>{description}</span>
        </div>
        <span className={`badge ${secretSet ? "badge--project" : "badge--feedback"}`}>
          {secretSetLabel ?? (secretSet ? t("settings.botSecretSet") : t("settings.botSecretMissing"))}
        </span>
      </div>
      <div className="bot-channel-card__switch">
        <ToggleSegment value={enabled} disabled={busy} onChange={onEnabled} />
      </div>
      <div className="bot-channel-card__simple">{children}</div>
      {advanced && (
        <details className="bot-advanced bot-advanced--compact">
          <summary>{t("settings.botAdvancedSettings")}</summary>
          <div className="bot-advanced__body">
            {advanced}
          </div>
        </details>
      )}
    </article>
  );
}

function BotSecretField({
  label,
  envName,
  secretSet,
  value,
  busy,
  onValue,
  onSave,
  onClear,
}: {
  label: ReactNode;
  envName: string;
  secretSet: boolean;
  value: string;
  busy: boolean;
  onValue: (value: string) => void;
  onSave: () => void;
  onClear: () => void;
}) {
  const t = useT();
  return (
    <BotCardField label={label}>
      <div className="bot-secret-row">
        <input
          className="mem-input"
          type="password"
          value={value}
          disabled={busy || !envName.trim()}
          placeholder={secretSet ? t("settings.botSecretReplace") : t("settings.botSecretPaste")}
          onChange={(e) => onValue(e.target.value)}
        />
        <button
          type="button"
          className="btn btn--small"
          disabled={busy || !envName.trim() || !value.trim()}
          onClick={onSave}
        >
          {secretSet ? t("settings.updateKeyAction") : t("settings.saveKey")}
        </button>
        {secretSet && (
          <InlineConfirmButton
            label={t("settings.clearKey")}
            confirmLabel={t("settings.confirmClearKey")}
            cancelLabel={t("common.cancel")}
            disabled={busy || !envName.trim()}
            danger
            onConfirm={onClear}
          />
        )}
      </div>
    </BotCardField>
  );
}

function BotCardField({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="bot-card-field">
      <span>{label}</span>
      <div>{children}</div>
    </div>
  );
}

function parseNonNegativeInt(value: string): number {
  const n = Number.parseInt(value.trim(), 10);
  return Number.isFinite(n) && n > 0 ? n : 0;
}

function sanitizeBotDraft(draft: BotSettingsView, promptModes: PromptMode[] = ENGINEERING_PROMPT_MODES): BotSettingsView {
  const bot = normalizeBotSettings(draft, promptModes);
  return {
    ...bot,
    enabled: true,
    model: bot.model.trim(),
    promptMode: "orca",
    workspaceRoot: bot.workspaceRoot.trim(),
    maxSteps: Math.max(0, Math.floor(bot.maxSteps || 0)),
    debounceMs: Math.max(0, Math.floor(bot.debounceMs || 0)),
    allowlist: {
      ...bot.allowlist,
      enabled: true,
      allowAll: true,
      qqUsers: uniqueStrings(bot.allowlist.qqUsers.map((v) => v.trim())),
      feishuUsers: uniqueStrings(bot.allowlist.feishuUsers.map((v) => v.trim())),
      weixinUsers: uniqueStrings(bot.allowlist.weixinUsers.map((v) => v.trim())),
      qqGroups: uniqueStrings(bot.allowlist.qqGroups.map((v) => v.trim())),
      feishuGroups: uniqueStrings(bot.allowlist.feishuGroups.map((v) => v.trim())),
      weixinGroups: uniqueStrings(bot.allowlist.weixinGroups.map((v) => v.trim())),
    },
    qq: {
      ...bot.qq,
      appId: bot.qq.appId.trim(),
      appSecretEnv: bot.qq.appSecretEnv.trim(),
      environment: bot.qq.environment === "sandbox" ? "sandbox" : "production",
    },
    feishu: {
      ...bot.feishu,
      enabled: false,
      domain: bot.feishu.domain === "lark" ? "lark" : "feishu",
      appId: bot.feishu.appId.trim(),
      appSecretEnv: bot.feishu.appSecretEnv.trim(),
      verificationToken: bot.feishu.verificationToken.trim(),
      mode: bot.feishu.mode === "websocket" ? "websocket" : "webhook",
      webhookPort: Math.max(0, Math.floor(bot.feishu.webhookPort || 0)),
    },
    weixin: {
      ...bot.weixin,
      enabled: true,
      accountId: bot.weixin.accountId.trim(),
      tokenEnv: bot.weixin.tokenEnv.trim(),
      apiBase: bot.weixin.apiBase.trim().replace(/\/+$/, ""),
    },
    connections: bot.connections.map(normalizeBotConnection).filter((conn) => conn.id && conn.provider),
  };
}

function ModelsSection({ s, busy, apply, backgroundApply }: ModelsSectionProps) {
  const t = useT();
  const { locale } = useI18n();
  const [subtab, setSubtab] = useState<"usage" | "access">("usage");
  const [visionCapabilities, setVisionCapabilities] = useState<VisionCapability[]>([]);
  const [probingVision, setProbingVision] = useState<string | null>(null);
  const autoRefreshKeyRef = useRef("");
  const refs = allRefs(s);
  const defaultRef = toRef(s.defaultModel, s);
  const plannerRef = toRef(s.plannerModel, s);
  const subagentRef = toRef(s.subagentModel, s);
  const plannerSelectRef = plannerRef === defaultRef ? "" : plannerRef;
  const [defaultProvider, defaultModel] = defaultRef.split("/");
  const defaultProviderView = s.providers.find((p) => p.name === defaultProvider);
  const currentModelLabel = defaultModel || defaultRef || t("common.none");
  const providerLabel = defaultProvider ? modelProviderLabel(defaultProvider, defaultProviderView, t) : t("common.none");
  const plannerLabel = plannerSelectRef || t("settings.plannerNone");
  const keyStatusLabel = defaultProviderView?.keySet ? t("settings.keySet") : t("settings.noKey");
  const modelRefKey = refs.join("|");

  useEffect(() => {
    let cancelled = false;
    const refresh = () => {
      void app.GetVisionCapabilities().then((items) => {
        if (!cancelled) setVisionCapabilities(items);
      }).catch(() => {
        if (!cancelled) setVisionCapabilities([]);
      });
    };
    refresh();
    const timer = window.setInterval(refresh, 3_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [modelRefKey]);

  const capabilityByRef = useMemo(() => new Map(visionCapabilities.map((item) => [item.modelRef, item])), [visionCapabilities]);
  const probeVision = async (modelRef: string) => {
    setProbingVision(modelRef);
    try {
      await backgroundApply(async () => {
        const result = await app.ProbeModelVision(modelRef);
        setVisionCapabilities((current) => [...current.filter((item) => item.modelRef !== modelRef), result]);
      });
    } finally {
      setProbingVision(null);
    }
  };
  const setVisionOverride = async (modelRef: string, override: string) => {
    await backgroundApply(async () => {
      const result = await app.SetVisionCapabilityOverride(modelRef, override);
      setVisionCapabilities((current) => [...current.filter((item) => item.modelRef !== modelRef), result]);
    });
  };
  const agent = normalizeAgentView(s.agent);
  const setAgentParams = (patch: Partial<typeof agent>) => {
    const next = normalizeAgentView({ ...agent, ...patch });
    return app.SetAgentParams(
      next.temperature,
      next.maxSteps,
      next.plannerMaxSteps,
      next.softCompactRatio,
      next.compactRatio,
      next.compactForceRatio,
      next.systemPrompt,
    );
  };

  useEffect(() => {
    if (subtab !== "usage") return;
    const groups = providerAccessGroups(s.providers.filter((p) => p.added), t);
    const candidates = groups
      .map((group) => {
        const provider = group.providers.find((p) => p.keySet && p.apiKeyEnv && p.baseUrl);
        return provider ? { group, provider } : null;
      })
      .filter((item): item is { group: ProviderAccessGroup; provider: ProviderView } => Boolean(item));
    const refreshKey = candidates.map(({ group, provider }) => `${group.id}:${provider.apiKeyEnv}`).join("|");
    if (!refreshKey || autoRefreshKeyRef.current === refreshKey) return;
    autoRefreshKeyRef.current = refreshKey;

    void backgroundApply(async () => {
      for (const { provider } of candidates) {
        // Background auto-refresh only protects a user-curated model list.
        // If the user hasn't specified any models, don't silently populate
        // the provider with every model from the API.
        if (!provider.models || provider.models.length === 0) continue;
        try {
          const fetched = await app.FetchProviderModels(provider);
          if (fetched.length === 0) continue;
          const models = mergedFetchedProviderModels(provider.models, fetched, { preserveCurated: true });
          const currentDefault = providerDefaultModel(provider.default, models);
          if (sameStringList(provider.models, models) && provider.default === currentDefault) continue;
          await app.UpdateProviderModels(provider.name, models, currentDefault);
        } catch {
          // Background discovery is opportunistic; manual refresh shows errors.
        }
      }
    });
  }, [backgroundApply, s.providers, subtab, t]);

  return (
    <>
      <div className="settings-subtabs">
        <button
          type="button"
          className={`settings-subtab${subtab === "usage" ? " settings-subtab--active" : ""}`}
          aria-selected={subtab === "usage"}
          onClick={() => setSubtab("usage")}
        >
          {t("settings.modelTab.usage")}
        </button>
        <button
          type="button"
          className={`settings-subtab${subtab === "access" ? " settings-subtab--active" : ""}`}
          aria-selected={subtab === "access"}
          onClick={() => setSubtab("access")}
        >
          {t("settings.modelTab.access")}
        </button>
      </div>

      {subtab === "usage" ? (
        <>
          <SettingsSection title={t("settings.modelUsage")}>
            <SettingsField label={t("settings.defaultModel")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={toRef(s.defaultModel, s)}
                disabled={busy}
                onPick={(ref) => void apply(() => app.SetDefaultModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.automationModel")} hint={t("settings.automationModelHint")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={toRef(s.automationModel, s)}
                disabled={busy}
                onPick={(ref) => void apply(() => app.SetAutomationModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.plannerModel")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={plannerSelectRef}
                disabled={busy}
                includeSameDefault
                onPick={(ref) => void apply(() => app.SetPlannerModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.subagentModel")}>
              <ModelPicker
                s={s}
                refs={refs}
                value={subagentRef}
                disabled={busy}
                emptyOptionLabel={t("settings.subagentModelDefault")}
                emptyOptionHint={t("common.auto")}
                onPick={(ref) => void apply(() => app.SetSubagentModel(ref))}
              />
            </SettingsField>

            <SettingsField label={t("settings.subagentEffort")} hint={t("settings.subagentHint")}>
              <select
                className="mem-select set-grow"
                value={s.subagentEffort || ""}
                disabled={busy}
                onChange={(e) => void apply(() => app.SetSubagentEffort(e.target.value))}
              >
                <option value="">{t("settings.subagentEffortDefault")}</option>
                {EFFORT_PRESETS.map((level) => (
                  <option key={level} value={level}>
                    {level}
                  </option>
                ))}
              </select>
            </SettingsField>

            <SettingsField label={t("settings.visionEnabled")} hint={t("settings.visionEnabledHint")}>
              <div className="set-seg">
                {(["off", "auto", "on"] as const).map((mode) => (
                  <button
                    key={mode}
                    className={`set-seg__btn${s.visionMode === mode ? " set-seg__btn--on" : ""}`}
                    disabled={busy}
                    onClick={() => void apply(() => app.SetVisionMode(mode))}
                  >
                    {t(`settings.visionMode.${mode}`)}
                  </button>
                ))}
              </div>
            </SettingsField>

            <SettingsField label={t("settings.visionCapabilities")} hint={t("settings.visionCapabilitiesHint")} stacked>
              <div className="settings-vision-capabilities">
                {refs.length === 0 ? (
                  <div className="settings-vision-capabilities__empty">{t("settings.visionCapabilitiesEmpty")}</div>
                ) : refs.map((ref) => {
                  const capability = capabilityByRef.get(ref);
                  const status = capability?.status || "unknown";
                  const statusLabel = t(`settings.visionStatus.${status}` as DictKey);
                  const automaticStatus = capability?.automaticStatus || (capability?.override && capability.override !== "auto" ? "unknown" : status);
                  const automaticStatusLabel = t(`settings.visionStatus.${automaticStatus}` as DictKey);
                  const checked = capability?.checkedAt
                    ? new Date(capability.checkedAt).toLocaleString(locale === "zh" ? "zh-CN" : "en-US")
                    : t("settings.visionNotChecked");
                  const isProbing = probingVision === ref || status === "probing";
                  return (
                    <div className="settings-vision-capabilities__row" key={ref}>
                      <div className="settings-vision-capabilities__model">
                        <strong title={ref}>{ref}</strong>
                        <span>{t("settings.visionAutomatic")}: {automaticStatusLabel} · {t("settings.visionEffective")}: {statusLabel} · {checked}</span>
                        {capability?.reason && <small title={capability.reason}>{capability.reason}</small>}
                      </div>
                      <div className="settings-vision-capabilities__actions">
                        <select
                          className="settings-vision-capabilities__override"
                          value={capability?.override || "auto"}
                          disabled={busy}
                          onChange={(event) => void setVisionOverride(ref, event.target.value)}
                          aria-label={t("settings.visionOverride")}
                        >
                          <option value="auto">{t("settings.visionOverride.auto")}</option>
                          <option value="supported">{t("settings.visionOverride.supported")}</option>
                          <option value="unsupported">{t("settings.visionOverride.unsupported")}</option>
                        </select>
                        <button
                          type="button"
                          className="btn btn--ghost settings-vision-capabilities__probe"
                          disabled={busy || isProbing}
                          onClick={() => void probeVision(ref)}
                          title={t("settings.visionProbe")}
                        >
                          <RefreshCw size={14} className={isProbing ? "settings-vision-capabilities__spin" : undefined} />
                          <span>{t("settings.visionProbe")}</span>
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </SettingsField>

            <div className="settings-model-current" aria-label={t("settings.modelCurrentStatus")}>
              <div>
                <span>{t("settings.modelCurrentStatus")}</span>
                <strong>{currentModelLabel}</strong>
              </div>
              <div className="settings-model-current__meta">
                <span>{providerLabel}</span>
                <span>{plannerLabel}</span>
                <span>{keyStatusLabel}</span>
              </div>
            </div>
          </SettingsSection>
          <SettingsSection title={t("settings.agentRuntime")} description={t("settings.agentRuntimeHint")}>
            <SettingsField label={t("settings.executorMaxSteps")} hint={t("settings.executorMaxStepsHint")}>
              <StepLimitControl
                value={agent.maxSteps}
                presets={[0, 10, 25, 50]}
                busy={busy}
                onChange={(next) => void apply(() => setAgentParams({ maxSteps: next }))}
              />
            </SettingsField>
            <SettingsField label={t("settings.plannerMaxSteps")} hint={plannerSelectRef ? t("settings.plannerMaxStepsHint") : t("settings.plannerMaxStepsDisabledHint")}>
              <StepLimitControl
                value={agent.plannerMaxSteps}
                presets={[6, 12, 25, 0]}
                busy={busy}
                onChange={(next) => void apply(() => setAgentParams({ plannerMaxSteps: next }))}
              />
            </SettingsField>
            <SettingsField label={t("settings.compactRatio")} hint={t("settings.compactRatioHint")}>
              <CompactRatioControl
                value={agent.compactRatio}
                busy={busy}
                onChange={(next) => void apply(() => setAgentParams(compactRatioPatch(next)))}
              />
            </SettingsField>
          </SettingsSection>
        </>
      ) : (
        <ProvidersSection s={s} busy={busy} apply={apply} />
      )}
    </>
  );
}

type ModelPickerOption = {
  ref: string;
  provider: string;
  model: string;
  providerView?: ProviderView;
};

function ModelPicker({
  s,
  refs,
  value,
  disabled,
  includeSameDefault = false,
  emptyOptionLabel,
  emptyOptionHint,
  onPick,
}: {
  s: SettingsView;
  refs: string[];
  value: string;
  disabled: boolean;
  includeSameDefault?: boolean;
  emptyOptionLabel?: string;
  emptyOptionHint?: string;
  onPick: (ref: string) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const triggerRef = useRef<HTMLButtonElement>(null);
  const q = query.trim().toLowerCase();
  const emptyLabel = includeSameDefault ? t("settings.plannerNone") : emptyOptionLabel;
  const emptyHint = includeSameDefault ? t("settings.plannerNoneHint") : emptyOptionHint;
  const emptyMeta = includeSameDefault ? t("settings.plannerNoneHintShort") : emptyOptionHint;
  const selected = refs.includes(value) ? modelOptionFromRef(value, s) : null;
  const selectedLabel = value === "" && emptyLabel
    ? emptyLabel
    : selected?.model || value || t("common.none");
  const selectedMeta = value === "" && emptyLabel
    ? emptyMeta || ""
    : selected
    ? modelOptionMeta(selected, t)
    : t("settings.noModelsConfigured");
  const emptyOptionVisible = Boolean(emptyLabel) && (!q || `${emptyLabel} ${emptyHint || ""}`.toLowerCase().includes(q));

  const groups = useMemo(() => {
    const providerOrder: string[] = [];
    const providerSeen = new Set<string>();
    for (const p of s.providers) {
      const id = providerGroupID(p);
      if (!providerSeen.has(id)) {
        providerOrder.push(id);
        providerSeen.add(id);
      }
    }
    const options = refs
      .map((ref) => modelOptionFromRef(ref, s))
      .filter((opt): opt is ModelPickerOption => Boolean(opt))
      .filter((opt) => !q || `${opt.ref} ${opt.provider} ${modelProviderLabel(opt.provider, opt.providerView, t)} ${opt.model}`.toLowerCase().includes(q));
    for (const opt of options) {
      const groupID = modelOptionGroupID(opt);
      if (!providerSeen.has(groupID)) {
        providerOrder.push(groupID);
        providerSeen.add(groupID);
      }
    }
    return providerOrder
      .map((groupID) => {
        const providerViews = s.providers.filter((p) => providerGroupID(p) === groupID);
        const firstProvider = providerViews[0];
        return {
          groupID,
          label: firstProvider ? providerGroupLabel(firstProvider, t) : groupID,
          keySet: providerViews.some((p) => p.keySet),
          options: uniqueModelOptions(options.filter((opt) => modelOptionGroupID(opt) === groupID)),
        };
      })
      .filter((group) => group.options.length > 0);
  }, [q, refs, s, t]);

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const pick = (ref: string) => {
    setOpen(false);
    if (ref !== value) onPick(ref);
  };

  return (
    <div className="settings-model-picker">
      <button
        ref={triggerRef}
        type="button"
        className="settings-model-picker__trigger"
        disabled={disabled || (!includeSameDefault && !emptyOptionLabel && refs.length === 0)}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((next) => !next)}
      >
        <span className="settings-model-picker__selected">
          <span>{selectedLabel}</span>
          <small>{selectedMeta}</small>
        </span>
        <ChevronDown size={16} className={`settings-model-picker__chev${open ? " settings-model-picker__chev--open" : ""}`} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="settings-model-picker__menu"
        placement="bottom"
        style={{ width: triggerRef.current?.getBoundingClientRect().width }}
      >
        <div className="settings-model-picker__search">
          <input
            value={query}
            placeholder={t("settings.searchModels")}
            onChange={(e) => setQuery(e.target.value)}
            autoFocus
          />
        </div>
        <div className="settings-model-picker__list" role="listbox">
          {emptyOptionVisible && (
            <button
              type="button"
              role="option"
              aria-selected={value === ""}
              className={`settings-model-picker__option settings-model-picker__option--pinned${value === "" ? " settings-model-picker__option--selected" : ""}`}
              onClick={() => pick("")}
            >
              <span>
                <strong>{emptyLabel}</strong>
                {emptyHint && <small>{emptyHint}</small>}
              </span>
              {value === "" && <Check size={14} />}
            </button>
          )}
          {groups.map((group) => (
            <div className="settings-model-picker__group" key={group.groupID}>
              <div className="settings-model-picker__group-title">
                <span>{group.label}</span>
                <small>{group.keySet ? t("settings.keySet") : t("settings.noKey")}</small>
              </div>
              {group.options.map((opt) => (
                <button
                  key={opt.ref}
                  type="button"
                  role="option"
                  aria-selected={opt.ref === value}
                  className={`settings-model-picker__option${opt.ref === value ? " settings-model-picker__option--selected" : ""}`}
                  onClick={() => pick(opt.ref)}
                >
                  <span>
                    <strong>{opt.model}</strong>
                    <small>{modelOptionMeta(opt, t)}</small>
                  </span>
                  {opt.ref === value && <Check size={14} />}
                </button>
              ))}
            </div>
          ))}
          {!emptyOptionVisible && groups.length === 0 && <div className="settings-model-picker__empty">{t("settings.noMatchingModels")}</div>}
        </div>
      </AnchoredPopover>
    </div>
  );
}

function modelOptionFromRef(ref: string, s: SettingsView): ModelPickerOption | null {
  if (!ref) return null;
  const [provider, ...modelParts] = ref.split("/");
  const model = modelParts.join("/") || ref;
  return {
    ref,
    provider,
    model,
    providerView: s.providers.find((p) => p.name === provider),
  };
}

function modelOptionMeta(option: ModelPickerOption, t: ReturnType<typeof useT>): string {
  const key = option.providerView?.keySet ? t("settings.keySet") : t("settings.noKey");
  return `${modelProviderLabel(option.provider, option.providerView, t)} · ${key}`;
}

function modelProviderLabel(provider: string, providerView: ProviderView | undefined, t: ReturnType<typeof useT>): string {
  return providerView ? providerGroupLabel(providerView, t) : provider;
}

function modelOptionGroupID(option: ModelPickerOption): string {
  return option.providerView ? providerGroupID(option.providerView) : `custom:${option.provider}`;
}

function uniqueModelOptions(options: ModelPickerOption[]): ModelPickerOption[] {
  const seen = new Set<string>();
  const out: ModelPickerOption[] = [];
  for (const option of options) {
    if (seen.has(option.model)) continue;
    seen.add(option.model);
    out.push(option);
  }
  return out;
}

function sameStringList(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((value, i) => value === b[i]);
}

function proxyModeLabel(mode: ProxyMode, t: ReturnType<typeof useT>): string {
  switch (mode) {
    case "auto":
      return t("settings.proxyMode.auto");
    case "custom":
      return t("settings.proxyMode.custom");
    case "off":
      return t("settings.proxyMode.off");
  }
}

function ProvidersSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const defaultProvider = toRef(s.defaultModel, s).split("/")[0];
  const [editing, setEditing] = useState<string | null>(null);
  const [adding, setAdding] = useState<AddProviderMode>(null);
  const [fetchingProvider, setFetchingProvider] = useState<string | null>(null);
  const [fetchResults, setFetchResults] = useState<Record<string, ProviderFetchResult>>({});
  const [modelDrafts, setModelDrafts] = useState<Record<string, ProviderModelDraft>>({});
  const groups = providerAccessGroups(s.providers.filter((p) => p.added), t);

  const setGroupFetchResult = (groupID: string, result: ProviderFetchResult | null) => {
    setFetchResults((prev) => {
      const next = { ...prev };
      if (result) next[groupID] = result;
      else delete next[groupID];
      return next;
    });
  };

  const setGroupModelDraft = (groupID: string, draft: ProviderModelDraft | null) => {
    setModelDrafts((prev) => {
      const next = { ...prev };
      if (draft) next[groupID] = draft;
      else delete next[groupID];
      return next;
    });
  };

  const modelDraftForFetch = (p: ProviderView, fetched: string[]): ProviderModelDraft => {
    const candidates = providerModelCandidates(p.models, fetched);
    const selected = mergedFetchedProviderModels(p.models, fetched, { preserveCurated: true });
    return {
      providerName: p.name,
      candidates,
      selected: candidates.filter((model) => selected.includes(model)),
    };
  };

  const updateModelDraftSelection = (groupID: string, nextSelected: (draft: ProviderModelDraft) => string[]) => {
    setModelDrafts((prev) => {
      const draft = prev[groupID];
      if (!draft) return prev;
      const selectedSet = new Set(nextSelected(draft));
      return {
        ...prev,
        [groupID]: {
          ...draft,
          selected: draft.candidates.filter((model) => selectedSet.has(model)),
        },
      };
    });
  };

  const refreshModels = async (group: ProviderAccessGroup, p: ProviderView) => {
    setFetchingProvider(group.id);
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    try {
      let fetched: string[];
      try {
        fetched = await app.FetchProviderModels(p);
      } catch (e) {
        setGroupFetchResult(group.id, {
          kind: "warn",
          text: t("settings.fetchModelsFailedForProvider", { provider: group.label, err: String((e as Error)?.message ?? e) }),
        });
        return;
      }
      if (fetched.length === 0) {
        setGroupFetchResult(group.id, {
          kind: "warn",
          text: t("settings.fetchModelsEmptyForProvider", { provider: group.label }),
        });
        return;
      }
      const draft = modelDraftForFetch(p, fetched);
      setGroupModelDraft(group.id, draft);
      setGroupFetchResult(group.id, {
        kind: "ok",
        text: t("settings.fetchModelsReadyForProvider", { provider: group.label, n: draft.candidates.length }),
      });
    } finally {
      setFetchingProvider(null);
    }
  };

  const refreshGroup = async (group: ProviderAccessGroup) => {
    const probe = group.providers[0];
    if (!probe) return;
    await refreshModels(group, probe);
  };

  const saveKeyEnvAndAutoRefresh = async (group: ProviderAccessGroup, apiKeyEnv: string, value: string) => {
    const probe = group.providers[0];
    if (!probe || !apiKeyEnv) return;
    setFetchingProvider(group.id);
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    try {
      await apply(async () => {
        await app.SetProviderKey(apiKeyEnv, value);
        try {
          const fetched = await app.FetchProviderModels({ ...probe, apiKeyEnv });
          if (fetched.length > 0) {
            const draft = modelDraftForFetch({ ...probe, apiKeyEnv }, fetched);
            setGroupModelDraft(group.id, draft);
            setGroupFetchResult(group.id, {
              kind: "ok",
              text: t("settings.fetchModelsReadyForProvider", { provider: group.label, n: draft.candidates.length }),
            });
            return;
          }
          setGroupFetchResult(group.id, {
            kind: "warn",
            text: t("settings.fetchModelsEmptyForProvider", { provider: group.label }),
          });
        } catch (e) {
          setGroupFetchResult(group.id, {
            kind: "warn",
            text: t("settings.fetchModelsAfterKeyFailedForProvider", { provider: group.label, err: String((e as Error)?.message ?? e) }),
          });
        }
      });
    } finally {
      setFetchingProvider(null);
    }
  };

  const saveProviderKey = async (group: ProviderAccessGroup, apiKeyEnv: string, value: string) => {
    if (!apiKeyEnv) return;
    setGroupFetchResult(group.id, null);
    setGroupModelDraft(group.id, null);
    await apply(() => app.SetProviderKey(apiKeyEnv, value));
  };

  const clearProviderKey = async (apiKeyEnv: string) => {
    if (!apiKeyEnv) return;
    await apply(() => app.ClearProviderKey(apiKeyEnv));
  };

  const saveModelDraft = async (group: ProviderAccessGroup) => {
    const draft = modelDrafts[group.id];
    const provider = draft ? group.providers.find((p) => p.name === draft.providerName) : null;
    const models = uniqueStrings(draft?.selected ?? []);
    if (!draft || !provider || models.length === 0) return;
    let saved = false;
    await apply(async () => {
      await app.UpdateProviderModels(provider.name, models, providerDefaultModel(provider.default, models));
      saved = true;
    });
    if (!saved) return;
    setGroupModelDraft(group.id, null);
    setGroupFetchResult(group.id, {
      kind: "ok",
      text: t("settings.enabledModelsSavedForProvider", { provider: group.label, n: models.length }),
    });
  };

  return (
    <SettingsSection
      title={t("settings.providerAccess")}
      description={t("settings.providerAccessHint")}
      actions={
        <button className="btn btn--small" disabled={busy || adding !== null} onClick={() => setAdding("official")}>
          {t("settings.addProvider")}
        </button>
      }
    >
      <div className="provider-access-grid">
        {groups.length === 0 && adding === null && (
          <div className="provider-empty">
            <strong>{t("settings.providerAccessEmptyTitle")}</strong>
            <span>{t("settings.providerAccessEmptyHint")}</span>
            <div className="provider-empty__actions">
              <button type="button" className="btn btn--small" disabled={busy} onClick={() => setAdding("official")}>
                {t("settings.addProvider.officialChoice")}
              </button>
              <button type="button" className="btn btn--small" disabled={busy} onClick={() => setAdding("custom")}>
                {t("settings.addProvider.customChoice")}
              </button>
            </div>
          </div>
        )}
        {adding !== null && (
          <AddProviderPanel
            mode={adding}
            kinds={s.providerKinds}
            busy={busy}
            onMode={setAdding}
            onCancel={() => setAdding(null)}
            onAddOfficial={(kind, key) => apply(() => app.AddOfficialProviderAccess(kind, key)).then(() => setAdding(null))}
            onAddCustom={(pv) => apply(() => app.SaveProvider(pv)).then(() => setAdding(null))}
          />
        )}
        {adding === null && groups.map((group) => (
          <ProviderAccessCard
            key={group.id}
            group={group}
            busy={busy}
            fetching={fetchingProvider === group.id || group.providers.some((p) => fetchingProvider === p.name)}
            fetchResult={fetchResults[group.id]}
            modelDraft={modelDrafts[group.id]}
            defaultProvider={defaultProvider}
            editing={editing}
            kinds={s.providerKinds}
            onEdit={setEditing}
            onCancelEdit={() => setEditing(null)}
            onSave={(pv) => apply(() => app.SaveProvider(pv)).then(() => {
              setEditing(null);
              setGroupModelDraft(group.id, null);
            })}
            onRefresh={() => void refreshGroup(group)}
            onToggleDraftModel={(model) => updateModelDraftSelection(group.id, (draft) => (
              draft.selected.includes(model)
                ? draft.selected.filter((candidate) => candidate !== model)
                : [...draft.selected, model]
            ))}
            onSelectAllDraftModels={() => updateModelDraftSelection(group.id, (draft) => draft.candidates)}
            onClearDraftModels={() => updateModelDraftSelection(group.id, () => [])}
            onCancelDraftModels={() => setGroupModelDraft(group.id, null)}
            onSaveDraftModels={() => void saveModelDraft(group)}
            onSaveEditorKey={(env, value) => group.builtIn ? saveProviderKey(group, env, value) : saveKeyEnvAndAutoRefresh(group, env, value)}
            onClearEditorKey={clearProviderKey}
            onDelete={(p) => apply(() => app.RemoveProviderAccess(p.name))}
          />
        ))}
      </div>
    </SettingsSection>
  );
}

type ProviderAccessGroup = {
  id: string;
  label: string;
  description: string;
  builtIn: boolean;
  providers: ProviderView[];
  apiKeyEnv: string;
  keySet: boolean;
  baseUrl: string;
  kind: string;
  models: string[];
};

type ProviderFetchResult = {
  kind: "ok" | "warn";
  text: string;
};

type ProviderModelDraft = {
  providerName: string;
  candidates: string[];
  selected: string[];
};

type AddProviderMode = null | "official" | "custom";
type OfficialProviderKind = "deepseek" | "mimo-api" | "mimo-token-plan";

const OFFICIAL_PROVIDER_CHOICES: Array<{ kind: OfficialProviderKind; labelKey: DictKey; descKey: DictKey; keyEnv: string }> = [
  { kind: "deepseek", labelKey: "settings.addProvider.official.deepseek", descKey: "settings.addProvider.official.deepseekDesc", keyEnv: "DEEPSEEK_API_KEY" },
  { kind: "mimo-api", labelKey: "settings.addProvider.official.mimoApi", descKey: "settings.addProvider.official.mimoApiDesc", keyEnv: "MIMO_API_KEY" },
  { kind: "mimo-token-plan", labelKey: "settings.addProvider.official.mimoTokenPlan", descKey: "settings.addProvider.official.mimoTokenPlanDesc", keyEnv: "MIMO_API_KEY" },
];

function AddProviderPanel({
  mode,
  kinds,
  busy,
  onMode,
  onCancel,
  onAddOfficial,
  onAddCustom,
}: {
  mode: AddProviderMode;
  kinds: string[];
  busy: boolean;
  onMode: (mode: AddProviderMode) => void;
  onCancel: () => void;
  onAddOfficial: (kind: OfficialProviderKind, key: string) => Promise<void>;
  onAddCustom: (p: ProviderView) => void | Promise<void>;
}) {
  const t = useT();
  const [officialKind, setOfficialKind] = useState<OfficialProviderKind>("deepseek");
  const [key, setKey] = useState("");
  const selected = OFFICIAL_PROVIDER_CHOICES.find((choice) => choice.kind === officialKind) ?? OFFICIAL_PROVIDER_CHOICES[0];

  const header = (
    <div className="provider-add-panel__head">
      <div>
        <strong>{t("settings.addProvider.chooseTitle")}</strong>
        <span>{t("settings.addProvider.chooseHint")}</span>
      </div>
      <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
        {t("common.cancel")}
      </button>
    </div>
  );
  const modeSwitch = (
    <div className="provider-add-segmented" role="tablist" aria-label={t("settings.addProvider.chooseTitle")}>
      <button
        type="button"
        role="tab"
        aria-selected={mode === "official"}
        className={mode === "official" ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
        disabled={busy}
        onClick={() => onMode("official")}
      >
        {t("settings.addProvider.officialChoice")}
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={mode === "custom"}
        className={mode === "custom" ? "provider-add-segmented__item provider-add-segmented__item--active" : "provider-add-segmented__item"}
        disabled={busy}
        onClick={() => onMode("custom")}
      >
        {t("settings.addProvider.customChoice")}
      </button>
    </div>
  );

  if (mode === "official") {
    return (
      <div className="provider-add-panel">
        {header}
        {modeSwitch}
        <div className="provider-add-panel__hint">{t("settings.addProvider.officialHint")}</div>
        <div className="provider-template-grid">
          {OFFICIAL_PROVIDER_CHOICES.map((choice) => (
            <button
              key={choice.kind}
              type="button"
              className={`provider-template-card${officialKind === choice.kind ? " provider-template-card--active" : ""}`}
              disabled={busy}
              onClick={() => setOfficialKind(choice.kind)}
            >
              <strong>{t(choice.labelKey)}</strong>
              <span>{t(choice.descKey)}</span>
            </button>
          ))}
        </div>
        <label className="set-label">{t("settings.providerKeyOptional")}</label>
        <input
          className="mem-input"
          type="password"
          placeholder={t("settings.setKey", { env: selected.keyEnv })}
          value={key}
          disabled={busy}
          onChange={(e) => setKey(e.target.value)}
        />
        <div className="prov-card__actions">
          <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn--primary btn--small"
            disabled={busy}
            onClick={() => void onAddOfficial(officialKind, key.trim())}
          >
            {t("settings.addProvider.confirm")}
          </button>
        </div>
      </div>
    );
  }

  if (mode === "custom") {
    return (
      <div className="provider-add-panel">
        {header}
        {modeSwitch}
        <div className="provider-add-panel__hint">{t("settings.addProvider.customHint")}</div>
        <ProviderEditor
          kinds={kinds}
          busy={busy}
          onCancel={onCancel}
          onSave={onAddCustom}
        />
      </div>
    );
  }
  return null;
}

function ProviderAccessCard({
  group,
  busy,
  fetching,
  fetchResult,
  modelDraft,
  defaultProvider,
  editing,
  kinds,
  onEdit,
  onCancelEdit,
  onSave,
  onRefresh,
  onToggleDraftModel,
  onSelectAllDraftModels,
  onClearDraftModels,
  onCancelDraftModels,
  onSaveDraftModels,
  onSaveEditorKey,
  onClearEditorKey,
  onDelete,
}: {
  group: ProviderAccessGroup;
  busy: boolean;
  fetching: boolean;
  fetchResult?: ProviderFetchResult;
  modelDraft?: ProviderModelDraft;
  defaultProvider: string;
  editing: string | null;
  kinds: string[];
  onEdit: (name: string) => void;
  onCancelEdit: () => void;
  onSave: (p: ProviderView) => void | Promise<void>;
  onRefresh: () => void;
  onToggleDraftModel: (model: string) => void;
  onSelectAllDraftModels: () => void;
  onClearDraftModels: () => void;
  onCancelDraftModels: () => void;
  onSaveDraftModels: () => void;
  onSaveEditorKey: (apiKeyEnv: string, value: string) => Promise<void>;
  onClearEditorKey?: (apiKeyEnv: string) => Promise<void>;
  onDelete?: (p: ProviderView) => Promise<void>;
}) {
  const t = useT();
  const editableProvider = group.providers[0];
  const isDefault = group.providers.some((p) => p.name === defaultProvider);
  const editingProvider = group.providers.find((p) => editing === p.name);
  const primaryProviderExpanded = Boolean(editableProvider && editing === editableProvider.name);
  const visibleModels = group.models.slice(0, 6);
  const hiddenModelCount = Math.max(0, group.models.length - visibleModels.length);
  return (
    <article className={`provider-access-card${group.builtIn ? " provider-access-card--builtin" : ""}`}>
      <div className="provider-access-card__head">
        <div className="provider-access-card__identity">
          <div className="provider-access-card__title">
            {group.label}
            <span className={`badge ${group.builtIn ? "badge--project" : "badge--neutral"}`}>
              {group.builtIn ? t("settings.builtinProviderBadge") : t("settings.customProviderBadge")}
            </span>
            <span className={`badge ${group.keySet ? "badge--project" : "badge--feedback"}`}>
              {group.keySet ? t("settings.keySet") : t("settings.noKey")}
            </span>
          </div>
          <div className="provider-access-card__desc">{group.description}</div>
        </div>
        <div className="provider-access-card__actions">
          {editableProvider && (
            <button
              className="btn btn--small"
              disabled={busy}
              aria-expanded={primaryProviderExpanded}
              onClick={() => primaryProviderExpanded ? onCancelEdit() : onEdit(editableProvider.name)}
            >
              {primaryProviderExpanded ? t("common.collapse") : t("settings.configureProvider")}
            </button>
          )}
          <button
            className="btn btn--small"
            disabled={busy || fetching || !group.baseUrl || !group.apiKeyEnv || !group.keySet}
            onClick={onRefresh}
          >
            {fetching ? t("settings.fetchingModels") : t("settings.fetchModels")}
          </button>
          {editableProvider && onDelete && (
            isDefault && !group.builtIn ? (
              <Tooltip label={t("settings.cantDeleteDefault")}>
                <button className="btn btn--small" disabled>{t("settings.removeProviderAccess")}</button>
              </Tooltip>
            ) : (
              <InlineConfirmButton
                label={t("settings.removeProviderAccess")}
                confirmLabel={group.builtIn ? t("settings.confirmRemoveProviderAccess") : t("settings.confirmDeleteProvider")}
                cancelLabel={t("common.cancel")}
                disabled={busy}
                danger={!group.builtIn}
                onConfirm={() => onDelete(editableProvider)}
              />
            )
          )}
        </div>
      </div>

      <div className="provider-access-meta">
        <span>{group.kind}</span>
        <span>{group.baseUrl}</span>
        <span>{group.apiKeyEnv || t("common.none")}</span>
      </div>

      <div className="provider-card-block">
        <div className="provider-card-block__label">{t(group.keySet ? "settings.enabledModels" : "settings.modelList")}</div>
        <div className="provider-model-chips" aria-label={t(group.keySet ? "settings.enabledModels" : "settings.modelList")}>
          {visibleModels.length > 0 ? visibleModels.map((model) => (
            <span className="provider-model-chip" key={model}>
              {model}
            </span>
          )) : <span className="provider-model-chip provider-model-chip--empty">{t("settings.noModelsConfigured")}</span>}
          {hiddenModelCount > 0 && (
            <span className="provider-model-chip provider-model-chip--more">
              {t("settings.moreModels", { n: hiddenModelCount })}
            </span>
          )}
        </div>
        {!group.keySet && (
          <div className="provider-card-status provider-card-status--warn">
            {t("settings.modelsRequireKey")}
          </div>
        )}
        {fetchResult && (
          <div className={`provider-card-status provider-card-status--${fetchResult.kind}`}>
            {fetchResult.text}
          </div>
        )}
      </div>

      {modelDraft && (
        <ProviderModelDraftPicker
          draft={modelDraft}
          busy={busy}
          fetching={fetching}
          onToggle={onToggleDraftModel}
          onSelectAll={onSelectAllDraftModels}
          onClear={onClearDraftModels}
          onCancel={onCancelDraftModels}
          onSave={onSaveDraftModels}
        />
      )}

      {group.providers.length > 1 && (
        <div className="provider-profiles">
          {group.providers.map((p) => {
            const profileExpanded = editing === p.name;
            return (
              <div className="provider-profile-row" key={p.name}>
                <span>{p.name}</span>
                <span>{p.models.join(", ") || t("common.none")}</span>
                <button
                  className="btn btn--small"
                  disabled={busy}
                  aria-expanded={profileExpanded}
                  onClick={() => profileExpanded ? onCancelEdit() : onEdit(p.name)}
                >
                  {profileExpanded ? t("common.collapse") : t("settings.configureProfile")}
                </button>
              </div>
            );
          })}
        </div>
      )}

      {editingProvider && (
        <ProviderEditor
          initial={editingProvider}
          kinds={kinds}
          busy={busy}
          onCancel={onCancelEdit}
          onSave={onSave}
          onSaveKey={onSaveEditorKey}
          onClearKey={onClearEditorKey}
        />
      )}
    </article>
  );
}

function ProviderModelDraftPicker({
  draft,
  busy,
  fetching,
  onToggle,
  onSelectAll,
  onClear,
  onCancel,
  onSave,
}: {
  draft: ProviderModelDraft;
  busy: boolean;
  fetching: boolean;
  onToggle: (model: string) => void;
  onSelectAll: () => void;
  onClear: () => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const t = useT();
  const [query, setQuery] = useState("");
  const selected = new Set(draft.selected);
  const q = query.trim().toLowerCase();
  const visibleCandidates = q
    ? draft.candidates.filter((model) => model.toLowerCase().includes(q))
    : draft.candidates;
  const disabled = busy || fetching;

  return (
    <div className="provider-model-draft">
      <div className="provider-model-draft__head">
        <div>
          <div className="provider-card-block__label">{t("settings.modelCandidates")}</div>
          <span>{t("settings.modelCandidatesSelected", { n: draft.selected.length })}</span>
        </div>
        <div className="provider-model-draft__tools">
          <button type="button" className="btn btn--small" disabled={disabled || draft.selected.length === draft.candidates.length} onClick={onSelectAll}>
            {t("settings.selectAllModels")}
          </button>
          <button type="button" className="btn btn--small" disabled={disabled || draft.selected.length === 0} onClick={onClear}>
            {t("settings.clearModelSelection")}
          </button>
        </div>
      </div>
      <input
        className="mem-input provider-model-draft__search"
        placeholder={t("settings.modelCandidateSearch")}
        value={query}
        disabled={disabled}
        onChange={(e) => setQuery(e.target.value)}
      />
      <div className="provider-model-draft__list" role="list" aria-label={t("settings.modelCandidates")}>
        {visibleCandidates.length > 0 ? visibleCandidates.map((model) => (
          <label className="provider-model-draft__option" key={model}>
            <input
              type="checkbox"
              checked={selected.has(model)}
              disabled={disabled}
              onChange={() => onToggle(model)}
            />
            <span>{model}</span>
          </label>
        )) : (
          <div className="provider-model-draft__empty">{t("settings.noMatchingCandidateModels")}</div>
        )}
      </div>
      <div className="provider-model-draft__actions">
        <button type="button" className="btn btn--small" disabled={disabled} onClick={onCancel}>
          {t("common.cancel")}
        </button>
        <button type="button" className="btn btn--primary btn--small" disabled={disabled || draft.selected.length === 0} onClick={onSave}>
          {t("settings.saveEnabledModels")}
        </button>
      </div>
    </div>
  );
}

function providerAccessGroups(providers: ProviderView[], t: ReturnType<typeof useT>): ProviderAccessGroup[] {
  const groups = new Map<string, ProviderAccessGroup>();
  for (const p of providers) {
    const id = providerGroupID(p);
    const builtIn = id.startsWith("builtin:");
    const existing = groups.get(id);
    if (existing) {
      existing.providers.push(p);
      existing.keySet = existing.keySet || p.keySet;
      existing.models = uniqueStrings([...existing.models, ...p.models]);
      continue;
    }
    groups.set(id, {
      id,
      label: providerGroupLabel(p, t),
      description: providerGroupDescription(p, t),
      builtIn,
      providers: [p],
      apiKeyEnv: p.apiKeyEnv,
      keySet: p.keySet,
      baseUrl: p.baseUrl,
      kind: p.kind,
      models: uniqueStrings(p.models),
    });
  }
  return Array.from(groups.values());
}

function providerBaseHost(baseUrl: string): string {
  try {
    return new URL(baseUrl).hostname.toLowerCase();
  } catch {
    return "";
  }
}

function canonicalOfficialProviderName(name: string): string {
  switch (name.trim()) {
    case "deepseek-flash":
    case "deepseek-pro":
      return "deepseek";
    case "mimo":
    case "xiaomi-mimo":
    case "xiaomi_mimo":
      return "mimo-api";
    case "mimo-pro":
    case "mimo-flash":
      return "mimo-token-plan";
    default:
      return name.trim();
  }
}

function officialProviderKind(p: ProviderView): string {
  if (!p.builtIn) return "";
  const name = canonicalOfficialProviderName(p.name);
  const host = providerBaseHost(p.baseUrl);
  if (name === "deepseek" && host === "api.deepseek.com") return "deepseek";
  if (name === "mimo-token-plan" && host === "token-plan-cn.xiaomimimo.com") return "mimo-token-plan";
  if (name === "mimo-api" && host === "api.xiaomimimo.com") return "mimo-api";
  return "";
}

function providerGroupID(p: ProviderView): string {
  const official = officialProviderKind(p);
  if (official) return `builtin:${official}`;
  return `custom:${p.name}`;
}

function providerGroupLabel(p: ProviderView, t?: ReturnType<typeof useT>): string {
  const id = providerGroupID(p);
  if (id === "builtin:deepseek") return t ? t("settings.providerLabel.deepseek") : "DeepSeek";
  if (id === "builtin:mimo-api") return t ? t("settings.providerLabel.mimoApi") : "Mimo API";
  if (id === "builtin:mimo-token-plan") return t ? t("settings.providerLabel.mimoTokenPlan") : "Mimo Token Plan";
  return p.name;
}

function providerGroupDescription(p: ProviderView, t: ReturnType<typeof useT>): string {
  const id = providerGroupID(p);
  if (id === "builtin:deepseek") return t("settings.providerDesc.deepseek");
  if (id === "builtin:mimo-api") return t("settings.providerDesc.mimoApi");
  if (id === "builtin:mimo-token-plan") return t("settings.providerDesc.mimoTokenPlan");
  return p.baseUrl;
}

function uniqueStrings(values: string[]): string[] {
  const out: string[] = [];
  for (const value of values) {
    if (value && !out.includes(value)) out.push(value);
  }
  return out;
}

function apiKeyEnvFromProviderName(name: string): string {
  const stem = name
    .trim()
    .toUpperCase()
    .replace(/[^A-Z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  return stem ? `${stem}_API_KEY` : "CUSTOM_API_KEY";
}

function ProviderEditor({
  initial,
  kinds,
  busy,
  onCancel,
  onSave,
  onSaveKey,
  onClearKey,
}: {
  initial?: ProviderView;
  kinds: string[];
  busy: boolean;
  onCancel: () => void;
  onSave: (p: ProviderView) => void;
  onSaveKey?: (apiKeyEnv: string, value: string) => Promise<void>;
  onClearKey?: (apiKeyEnv: string) => Promise<void>;
}) {
  const t = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [kind, setKind] = useState(initial?.kind ?? kinds[0] ?? "openai");
  const [baseUrl, setBaseUrl] = useState(initial?.baseUrl ?? "");
  const [models, setModels] = useState((initial?.models ?? []).join(", "));
  const [modelsUrl] = useState(initial?.modelsUrl ?? "");
  const [apiKeyEnv, setApiKeyEnv] = useState(initial?.apiKeyEnv ?? "");
  const [keyDraft, setKeyDraft] = useState("");
  const [balanceUrl, setBalanceUrl] = useState(initial?.balanceUrl ?? "");
  // Empty when unset so the placeholder (and its "0 = default" hint) reads instead
  // of a bare "0"; saved back as 0.
  const [ctx, setCtx] = useState(initial?.contextWindow ? String(initial.contextWindow) : "");
  const [reasoningProtocol, setReasoningProtocol] = useState(normalizeReasoningProtocol(initial?.reasoningProtocol));
  const [supportedEfforts, setSupportedEfforts] = useState<string[]>(initial?.supportedEfforts ?? []);
  const [customEffortDraft, setCustomEffortDraft] = useState("");
  const [defaultEffort, setDefaultEffort] = useState(initial?.defaultEffort ?? "");
  const [fetchingModels, setFetchingModels] = useState(false);
  const [fetchStatus, setFetchStatus] = useState<string | null>(null);
  const [fetchErr, setFetchErr] = useState<string | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const builtIn = initial?.builtIn ?? false;
  const isNewCustomProvider = !initial;

  // Offer the kinds the kernel actually registered; if the stored kind is a
  // legacy/unknown one, keep it as an option so editing doesn't silently change it.
  const kindOptions = kind && !kinds.includes(kind) ? [kind, ...kinds] : kinds;

  // Split supportedEfforts into the 5 canonical presets (for checkbox UI) and
  // any user-added custom names (rendered as removable chips). The preset order
  // is fixed; custom names keep insertion order.
  const presetEfforts = supportedEfforts.filter((e) => EFFORT_PRESETS.includes(e));
  const customEfforts = supportedEfforts.filter((e) => !EFFORT_PRESETS.includes(e));

  const togglePreset = (level: string) => {
    const has = presetEfforts.includes(level);
    const nextPresets = has ? presetEfforts.filter((e) => e !== level) : [...presetEfforts, level];
    setSupportedEfforts([...nextPresets, ...customEfforts]);
    // If the removed preset was the default, fall back to "auto" (empty string).
    if (has && defaultEffort === level) setDefaultEffort("");
  };

  const addCustomEffort = () => {
    const v = customEffortDraft.trim().toLowerCase();
    if (!v || supportedEfforts.includes(v)) {
      setCustomEffortDraft("");
      return;
    }
    setSupportedEfforts([...presetEfforts, ...customEfforts, v]);
    setCustomEffortDraft("");
  };

  const removeCustomEffort = (level: string) => {
    setSupportedEfforts(supportedEfforts.filter((e) => e !== level));
    if (defaultEffort === level) setDefaultEffort("");
  };

  const fetchModels = async () => {
    setFetchingModels(true);
    setFetchStatus(null);
    setFetchErr(null);
    try {
      const effectiveApiKeyEnv = apiKeyEnv.trim() || apiKeyEnvFromProviderName(name);
      if (!apiKeyEnv.trim()) setApiKeyEnv(effectiveApiKeyEnv);
      if (keyDraft.trim()) await app.SetProviderKey(effectiveApiKeyEnv, keyDraft.trim());
      const fetched = await app.FetchProviderModels({
        name: name.trim() || t("settings.newProviderDraftName"),
        builtIn: initial?.builtIn ?? false,
        added: initial?.added ?? true,
        kind: kind.trim() || kinds[0] || "openai",
        baseUrl: baseUrl.trim(),
        modelsUrl,
        models: [],
        default: "",
        apiKeyEnv: effectiveApiKeyEnv,
        keySet: Boolean(keyDraft.trim()) || (initial?.keySet ?? false),
        balanceUrl: balanceUrl.trim(),
        contextWindow: Number(ctx) || 0,
        modelContextWindows: initial?.modelContextWindows,
        reasoningProtocol,
        supportedEfforts,
        defaultEffort,
      });
      if (fetched.length === 0) throw new Error(t("settings.fetchModelsEmpty"));
      setModels(fetched.join(", "));
      if (keyDraft.trim()) setKeyDraft("");
      setDefaultEffort((v) => v);
      setFetchStatus(t("settings.fetchModelsSuccess", { n: fetched.length }));
    } catch (e) {
      setFetchErr(String((e as Error)?.message ?? e));
    } finally {
      setFetchingModels(false);
    }
  };

  const save = async () => {
    const ms = models
      .split(",")
      .map((m) => m.trim())
      .filter(Boolean);
    const effectiveApiKeyEnv = apiKeyEnv.trim() || apiKeyEnvFromProviderName(name);
    if (keyDraft.trim()) await app.SetProviderKey(effectiveApiKeyEnv, keyDraft.trim());
    onSave({
      name: name.trim(),
      builtIn: initial?.builtIn ?? false,
      added: initial?.added ?? true,
      kind: kind.trim() || kinds[0] || "openai",
      baseUrl: baseUrl.trim(),
      models: ms,
      default: ms[0] ?? "",
      apiKeyEnv: effectiveApiKeyEnv,
      modelsUrl,
      keySet: Boolean(keyDraft.trim()) || (initial?.keySet ?? false),
      balanceUrl: balanceUrl.trim(),
      contextWindow: Number(ctx) || 0,
      modelContextWindows: initial?.modelContextWindows,
      reasoningProtocol,
      supportedEfforts,
      // Clear the stored default if no levels are selected; the backend's
      // NormalizeEffort would otherwise silently ignore an unsupported value.
      defaultEffort: supportedEfforts.length > 0 ? defaultEffort : "",
    });
  };

  if (builtIn) {
    const keyEnv = initial?.apiKeyEnv.trim() ?? "";
    return (
      <div className="provider-editor provider-editor--builtin provider-editor--key-only">
        {initial && onSaveKey && keyEnv && (
          <>
            <div className="provider-key-status provider-key-status--managed provider-key-status--compact">
              <span>{initial.keySet ? t("settings.configuredKey", { env: keyEnv }) : t("settings.notConfiguredKey", { env: keyEnv })}</span>
              {initial.keySet && onClearKey && (
                <InlineConfirmButton
                  label={t("settings.clearKey")}
                  confirmLabel={t("settings.confirmClearKey")}
                  cancelLabel={t("common.cancel")}
                  disabled={busy}
                  danger
                  onConfirm={() => onClearKey(keyEnv)}
                />
              )}
            </div>
            <KeyField
              apiKeyEnv={keyEnv}
              busy={busy}
              keySet={initial.keySet}
              onSet={(env, value) => onSaveKey(env, value)}
            />
          </>
        )}
      </div>
    );
  }

  const modelNames = models
    .split(",")
    .map((m) => m.trim())
    .filter(Boolean);
  const canFetch = Boolean(name.trim() && baseUrl.trim() && (keyDraft.trim() || apiKeyEnv.trim()));

  const protocolField = initial ? (
    <select className="mem-select" value={kind} onChange={(e) => setKind(e.target.value)}>
      {kindOptions.map((k) => (
        <option key={k} value={k}>
          {k === "openai" ? t("settings.providerProtocolOpenAI") : k}
        </option>
      ))}
    </select>
  ) : (
    <div className="provider-readonly-field provider-readonly-field--stacked" aria-readonly="true">
      <strong>{t("settings.providerProtocolOpenAI")}</strong>
      <span>{t("settings.providerProtocolOpenAIHint")}</span>
    </div>
  );

  const advancedFields = (
    <details className="provider-editor-advanced" open={advancedOpen} onToggle={(e) => setAdvancedOpen(e.currentTarget.open)}>
      <summary>{t("settings.providerAdvancedSettings")}</summary>
      <div className="provider-editor-advanced__body">
        <label className="set-label">{t("settings.providerApiKeyEnv")}</label>
        <input
          className="mem-input"
          placeholder={apiKeyEnvFromProviderName(name)}
          value={apiKeyEnv}
          onChange={(e) => setApiKeyEnv(e.target.value)}
        />
        <div className="mem-hint">{t("settings.providerApiKeyEnvHint")}</div>
        <label className="set-label">{t("settings.providerBalanceUrl")}</label>
        <input className="mem-input" placeholder={t("settings.balanceUrlPlaceholder")} value={balanceUrl} onChange={(e) => setBalanceUrl(e.target.value)} />
        <div className="mem-hint">{t("settings.balanceUrlHint")}</div>
        <label className="set-label">{t("settings.providerContextWindow")}</label>
        <input className="mem-input" placeholder={t("settings.contextWindowPlaceholder")} value={ctx} onChange={(e) => setCtx(e.target.value)} inputMode="numeric" />
        <div className="mem-hint">{t("settings.contextWindowHint")}</div>
        <label className="set-label">{t("settings.reasoningProtocol")}</label>
        <select className="mem-select" value={reasoningProtocol} onChange={(e) => setReasoningProtocol(e.target.value)}>
          {REASONING_PROTOCOLS.map((protocol) => (
            <option key={protocol || "auto"} value={protocol}>
              {reasoningProtocolLabel(protocol, t)}
            </option>
          ))}
        </select>
        <div className="mem-hint">{t("settings.reasoningProtocolHint")}</div>
        <label className="set-label">{t("settings.supportedEfforts")}</label>
        {EFFORT_PRESETS.map((level) => (
          <label key={level} className="set-check">
            <input
              type="checkbox"
              checked={presetEfforts.includes(level)}
              onChange={() => togglePreset(level)}
            />
            {level}
          </label>
        ))}
        <div className="set-row">
          <input
            className="mem-input set-grow"
            placeholder={t("settings.supportedEffortsCustomPlaceholder")}
            value={customEffortDraft}
            onChange={(e) => setCustomEffortDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addCustomEffort();
              }
            }}
          />
          <button
            type="button"
            className="btn btn--small"
            disabled={
              !customEffortDraft.trim() || supportedEfforts.includes(customEffortDraft.trim().toLowerCase())
            }
            onClick={addCustomEffort}
          >
            {t("common.add")}
          </button>
        </div>
        {customEfforts.length > 0 && (
          <div className="set-rules__chips">
            {customEfforts.map((level) => (
              <span className="set-rule" key={level}>
                {level}
                <Tooltip label={t("common.delete")}>
                  <button
                    type="button"
                    className="set-rule__x"
                    disabled={busy}
                    onClick={() => removeCustomEffort(level)}
                  >
                    ×
                  </button>
                </Tooltip>
              </span>
            ))}
          </div>
        )}
        <div className="mem-hint">{t("settings.supportedEffortsHint")}</div>
        <label className="set-label">{t("settings.defaultEffort")}</label>
        {supportedEfforts.length > 0 ? (
          <select
            className="mem-select"
            value={defaultEffort}
            onChange={(e) => setDefaultEffort(e.target.value)}
          >
            <option value="">{t("settings.defaultEffortAuto")}</option>
            {supportedEfforts.map((level) => (
              <option key={level} value={level}>
                {level}
              </option>
            ))}
          </select>
        ) : (
          <select className="mem-select" value="" disabled>
            <option value="">{t("settings.defaultEffortAuto")}</option>
          </select>
        )}
        <div className="mem-hint">{t("settings.defaultEffortHint")}</div>
      </div>
    </details>
  );

  return (
    <div className={`provider-editor${isNewCustomProvider ? " provider-editor--wizard" : ""}`}>
      <label className="set-label">{t("settings.customProviderName")}</label>
      <input className="mem-input" placeholder={t("settings.customProviderNamePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} disabled={!!initial} />
      <label className="set-label">{t("settings.providerProtocol")}</label>
      {protocolField}
      <label className="set-label">{t("settings.providerBaseUrlLabel")}</label>
      <input className="mem-input" placeholder={t("settings.providerBaseUrl")} value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
      {!initial && (
        <>
          <label className="set-label">{t("settings.providerKey")}</label>
          <input
            className="mem-input"
            type="password"
            placeholder={t("settings.providerKeyPlaceholder")}
            value={keyDraft}
            onChange={(e) => setKeyDraft(e.target.value)}
          />
        </>
      )}
      {initial && onSaveKey && apiKeyEnv.trim() && (
        <>
          <label className="set-label">{t("settings.providerKey")}</label>
          <KeyField
            apiKeyEnv={apiKeyEnv.trim()}
            busy={busy || fetchingModels}
            keySet={initial.keySet}
            onSet={(env, value) => onSaveKey(env, value)}
          />
        </>
      )}
      <div className="provider-model-fetch-row">
        <button
          type="button"
          className="btn btn--small"
          disabled={busy || fetchingModels || !canFetch}
          onClick={() => void fetchModels()}
        >
          {fetchingModels ? t("settings.fetchingModels") : t("settings.testFetchModels")}
        </button>
        <span>{t("settings.testFetchModelsHint")}</span>
      </div>
      {fetchStatus && <div className="provider-fetch-status provider-fetch-status--ok">{fetchStatus}</div>}
      {fetchErr && <div className="provider-fetch-status provider-fetch-status--error">{fetchErr}</div>}
      {modelNames.length > 0 && (
        <div className="provider-card-block">
          <div className="provider-card-block__label">{t("settings.availableModels")}</div>
          <div className="provider-model-chips">
            {modelNames.slice(0, 8).map((model) => (
              <span className="provider-model-chip" key={model}>{model}</span>
            ))}
            {modelNames.length > 8 && (
              <span className="provider-model-chip provider-model-chip--more">{t("settings.moreModels", { n: modelNames.length - 8 })}</span>
            )}
          </div>
        </div>
      )}
      <label className="set-label">{t("settings.manualModels")}</label>
      <input className="mem-input" placeholder={t("settings.providerModels")} value={models} onChange={(e) => setModels(e.target.value)} />
      <div className="mem-hint">{t("settings.manualModelsHint")}</div>
      {advancedFields}
      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={() => void save()} disabled={busy || !name.trim() || !baseUrl.trim() || !models.trim()}>
          {t("common.save")}
        </button>
      </div>
    </div>
  );
}

function KeyField({
  apiKeyEnv,
  busy,
  keySet = false,
  onSet,
}: {
  apiKeyEnv: string;
  busy: boolean;
  keySet?: boolean;
  onSet: (apiKeyEnv: string, value: string) => Promise<void>;
}) {
  const t = useT();
  const [val, setVal] = useState("");
  if (!apiKeyEnv) return null;
  return (
    <div className="set-key">
      <input
        className="mem-input"
        type="password"
        placeholder={t(keySet ? "settings.updateKey" : "settings.setKey", { env: apiKeyEnv })}
        value={val}
        onChange={(e) => setVal(e.target.value)}
      />
      <button
        className="btn btn--small"
        disabled={busy || !val.trim()}
        onClick={() => {
          void onSet(apiKeyEnv, val.trim());
          setVal("");
        }}
      >
        {t(keySet ? "settings.updateKeyAction" : "settings.saveKey")}
      </button>
    </div>
  );
}

function PermissionsSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const refs = allRefs(s);
  return (
    <>
    <SettingsSection title={t("settings.permissions")} description={t("settings.permissionsModeHint")}>
      <SettingsField label={t("settings.automationFullAccess")} hint={t("settings.automationFullAccessHint")}>
        <ToggleSegment
          value={s.automationFullAccessApproved}
          disabled={busy}
          onChange={(enabled) => void apply(() => app.SetAutomationFullAccess(enabled))}
        />
      </SettingsField>
      <SettingsField label={t("settings.writerMode")}>
        <select
          className="mem-select set-grow"
          value={s.permissions.mode}
          disabled={busy}
          onChange={(e) => void apply(() => app.SetPermissionMode(e.target.value))}
        >
          <option value="ask">{t("settings.modeAsk")}</option>
          <option value="allow">{t("settings.modeAllow")}</option>
          <option value="deny">{t("settings.modeDeny")}</option>
        </select>
      </SettingsField>
      <SettingsField label={t("settings.autoReviewModel")} hint={t("settings.autoReviewModelHint")}>
        <ModelPicker
          s={s}
          refs={refs}
          value={toRef(s.permissions.autoReviewModel, s)}
          disabled={busy}
          emptyOptionLabel={t("settings.autoReviewModelCurrent")}
          emptyOptionHint={t("settings.autoReviewModelCurrentHint")}
          onPick={(ref) => void apply(() => app.SetAutoReviewModel(ref))}
        />
      </SettingsField>
    </SettingsSection>
    <SettingsSection title={t("settings.permissionRules")} description={t("settings.ruleForm")}>
      <div className="set-rules-grid">
        {(["deny", "ask", "allow"] as const).map((list) => (
          <RuleList
            key={list}
            list={list}
            rules={s.permissions[list]}
            busy={busy}
            onAdd={(rule) => apply(() => app.AddPermissionRule(list, rule))}
            onRemove={(rule) => apply(() => app.RemovePermissionRule(list, rule))}
          />
        ))}
      </div>
    </SettingsSection>
    </>
  );
}

function RuleList({
  list,
  rules,
  busy,
  onAdd,
  onRemove,
}: {
  list: string;
  rules: string[];
  busy: boolean;
  onAdd: (rule: string) => Promise<void>;
  onRemove: (rule: string) => Promise<void>;
}) {
  const t = useT();
  const [draft, setDraft] = useState("");
  const add = () => {
    const r = draft.trim();
    if (r) {
      void onAdd(r);
      setDraft("");
    }
  };
  return (
    <div className="set-rules">
      <div className="set-rules__head">
        <div className="set-rules__label">{ruleListLabel(list, t)}</div>
        {ruleListHint(list, t) && <div className="set-rules__hint">{ruleListHint(list, t)}</div>}
      </div>
      <div className="set-rules__chips">
        {rules.length === 0 && <span className="mem-empty">{t("common.none")}</span>}
        {rules.map((r) => (
          <span className="set-rule" key={r}>
            {r}
            <Tooltip label={t("common.delete")}>
              <button className="set-rule__x" disabled={busy} onClick={() => void onRemove(r)}>
                ✕
              </button>
            </Tooltip>
          </span>
        ))}
      </div>
      <div className="set-rules__add">
        <input
          className="mem-input"
          placeholder={t("settings.addRule", { list })}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") add();
          }}
        />
        <button className="btn btn--small" disabled={busy || !draft.trim()} onClick={add}>
          {t("common.add")}
        </button>
      </div>
    </div>
  );
}

function ruleListLabel(list: string, t: ReturnType<typeof useT>): string {
  switch (list) {
    case "deny":
      return t("settings.ruleDeny");
    case "ask":
      return t("settings.ruleAsk");
    case "allow":
      return t("settings.ruleAllow");
    case "allow_write":
      return t("settings.ruleAllowWrite");
    default:
      return list;
  }
}

function ruleListHint(list: string, t: ReturnType<typeof useT>): string {
  switch (list) {
    case "deny":
      return t("settings.ruleDenyHint");
    case "ask":
      return t("settings.ruleAskHint");
    case "allow":
      return t("settings.ruleAllowHint");
    default:
      return "";
  }
}

function SandboxSection({ s, busy, apply }: SectionProps) {
  const t = useT();
  const sb = s.sandbox;
  const [root, setRoot] = useState(sb.workspaceRoot);
  const set = (next: Partial<typeof sb>) =>
    apply(() => app.SetSandbox(next.bash ?? sb.bash, next.network ?? sb.network, next.workspaceRoot ?? sb.workspaceRoot, next.allowWrite ?? sb.allowWrite));

  return (
    <SettingsSection title={t("settings.sandboxTitle")}>
      <SettingsField label={t("settings.bashSandbox")}>
        <select className="mem-select set-grow" value={sb.bash} disabled={busy} onChange={(e) => void set({ bash: e.target.value })}>
          <option value="enforce">{t("settings.bashEnforce")}</option>
          <option value="off">{t("settings.bashOff")}</option>
        </select>
      </SettingsField>
      <SettingsField label={t("settings.allowNetwork")}>
        <label className="set-check set-check--inline">
          <input type="checkbox" checked={sb.network} disabled={busy} onChange={(e) => void set({ network: e.target.checked })} />
          {t("settings.allowNetwork")}
        </label>
      </SettingsField>
      <SettingsField label={t("settings.workspaceRoot")}>
        <input
          className="mem-input set-grow"
          placeholder={t("settings.workspaceDefault")}
          value={root}
          disabled={busy}
          onChange={(e) => setRoot(e.target.value)}
          onBlur={() => root !== sb.workspaceRoot && void set({ workspaceRoot: root })}
        />
      </SettingsField>
      <RuleList
        list="allow_write"
        rules={sb.allowWrite}
        busy={busy}
        onAdd={(d) => set({ allowWrite: [...sb.allowWrite, d] })}
        onRemove={(d) => set({ allowWrite: sb.allowWrite.filter((x) => x !== d) })}
      />
    </SettingsSection>
  );
}

function AppearanceSection({
  busy,
  uiStyle,
  textSize,
  fontFamily,
  onTextSize,
  onFontFamily,
  onUIStyle,
	restartStyle,
	onRestartLater,
	onRestartNow,
}: {
  busy: boolean;
  uiStyle: UIStyle;
  textSize: TextSize;
  fontFamily: FontFamily;
  onTextSize: (size: TextSize) => void;
  onFontFamily: (font: FontFamily) => void;
  onUIStyle: (style: UIStyle) => void;
	restartStyle: UIStyle | null;
	onRestartLater: () => void;
	onRestartNow: () => void;
}) {
  const t = useT();
  return (
    <SettingsSection title={t("settings.appearance")}>
      <SettingsField label={t("settings.uiStyle")} hint={t("settings.uiStyleHint")}>
        <div className="set-seg">
          {UI_STYLES.map((style) => (
            <button
              key={style}
              type="button"
              disabled={busy}
              className={`set-seg__btn${uiStyle === style ? " set-seg__btn--on" : ""}`}
              onClick={() => onUIStyle(style)}
            >
              {t(style === "modern" ? "settings.uiStyleModern" : "settings.uiStyleClassic")}
            </button>
          ))}
        </div>
      </SettingsField>
	  {restartStyle && (
		<div className="settings-restart-notice" role="status">
		  <div>
			<strong>{t("settings.uiStyleRestartTitle")}</strong>
			<span>{t("settings.uiStyleRestartBody")}</span>
		  </div>
		  <div className="settings-restart-notice__actions">
			<button type="button" className="btn btn--ghost" onClick={onRestartLater}>{t("settings.restartLater")}</button>
			<button type="button" className="btn btn--primary" onClick={onRestartNow}>{t("settings.restartNow")}</button>
		  </div>
		</div>
	  )}
      <SettingsField label={t("settings.textSize")}>
        <div className="set-seg">
          {TEXT_SIZES.map((size) => (
            <button
              key={size}
              className={`set-seg__btn${textSize === size ? " set-seg__btn--on" : ""}`}
              onClick={() => onTextSize(size)}
            >
              {textSizeName(size, t)}
            </button>
          ))}
        </div>
      </SettingsField>
      <SettingsField label={t("settings.fontFamily")}>
        <div className="set-seg">
          {FONT_FAMILIES.map((font) => (
            <button
              key={font}
              className={`set-seg__btn${fontFamily === font ? " set-seg__btn--on" : ""}`}
              onClick={() => onFontFamily(font)}
            >
              {fontFamilyName(font, t)}
            </button>
          ))}
        </div>
      </SettingsField>
    </SettingsSection>
  );
}

function normalizeAgentView(agent: Partial<SettingsView["agent"]> | undefined) {
  return {
    temperature: Number.isFinite(agent?.temperature) ? Number(agent?.temperature) : 0,
    maxSteps: Math.max(0, Math.floor(agent?.maxSteps ?? 0)),
    plannerMaxSteps: Math.max(0, Math.floor(agent?.plannerMaxSteps ?? 12)),
    softCompactRatio: clampNumber(agent?.softCompactRatio ?? 0.5, 0.1, 0.85),
    compactRatio: clampNumber(agent?.compactRatio ?? 0.8, 0.2, 0.95),
    compactForceRatio: clampNumber(agent?.compactForceRatio ?? 0.9, 0.3, 0.98),
    systemPrompt: agent?.systemPrompt ?? "",
  };
}

function compactRatioPatch(compactRatio: number) {
  const trigger = clampNumber(compactRatio, 0.2, 0.95);
  return {
    compactRatio: trigger,
    softCompactRatio: clampNumber(Math.min(trigger - 0.1, 0.5), 0.1, 0.85),
    compactForceRatio: clampNumber(Math.max(trigger + 0.05, 0.9), 0.3, 0.98),
  };
}

function clampNumber(v: number, min: number, max: number): number {
  if (!Number.isFinite(v)) return min;
  return Math.max(min, Math.min(max, v));
}

function textSizeName(size: TextSize, t: ReturnType<typeof useT>): string {
  switch (size) {
    case "small":
      return t("settings.textSizeSmall");
    case "default":
      return t("settings.textSizeDefault");
    case "large":
      return t("settings.textSizeLarge");
    case "xlarge":
      return t("settings.textSizeXLarge");
  }
}

function fontFamilyName(font: FontFamily, t: ReturnType<typeof useT>): string {
  switch (font) {
    case "heiti":
      return t("settings.fontFamilyHeiti");
    case "dengxian":
      return t("settings.fontFamilyDengXian");
    case "simsun":
      return t("settings.fontFamilySimSun");
    case "yahei-ui":
      return t("settings.fontFamilyYaHeiUI");
  }
}

