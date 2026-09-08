import { CheckCircle2, Clock3, Download, ExternalLink, FolderOpen, PackageCheck, RefreshCw, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { app, onUpdaterProgress, openExternal } from "../lib/bridge";
import type { UpdateInfo, UpdateProgress, UpdateProgressPhase } from "../lib/types";
import { useT } from "../lib/i18n";
import { flushComposerDrafts, isComposerDraftFlushError } from "../lib/composerDraftPersistence";
import { canCancelUpdateDownload, runReadyUpdateAction } from "../lib/updaterBannerState";
import "./updateBanner.css";

const ACTIVE_PHASES: UpdateProgressPhase[] = ["downloading", "verifying", "applying"];
const KNOWN_PHASES: UpdateProgressPhase[] = ["idle", ...ACTIVE_PHASES, "ready", "cancelled", "error"];

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error || "");
}

function isActiveTaskRefusal(error: unknown): boolean {
  return /(already|active|in progress|busy|running|task|turn|job|automation|computer control|wait|正在下载|任务|回合|请等待|自动化|电脑控制)/i.test(errorText(error));
}

function normalizedVersion(value: string): string {
  return value.trim().replace(/^v/i, "");
}

function normalizeProgress(value: UpdateProgress | null | undefined, version: string): UpdateProgress | null {
  if (!value || typeof value !== "object") return null;
  if (value.version && normalizedVersion(value.version) !== normalizedVersion(version)) return null;
  const rawPhase = String(value.phase || "idle");
  const phase = rawPhase === "done" ? "ready" : rawPhase;
  if (!KNOWN_PHASES.includes(phase as UpdateProgressPhase)) return null;
  return {
    phase: phase as UpdateProgressPhase,
    received: Number.isFinite(value.received) ? Math.max(0, value.received) : 0,
    total: Number.isFinite(value.total) ? Math.max(0, value.total) : 0,
    ...(value.err ? { err: value.err } : {}),
    version: value.version || version,
    ...(typeof value.canSelfUpdate === "boolean" ? { canSelfUpdate: value.canSelfUpdate } : {}),
    source: value.source,
    sources: value.sources,
    speedBps: Number.isFinite(value.speedBps) ? Math.max(0, value.speedBps!) : 0,
    etaSeconds: Number.isFinite(value.etaSeconds) ? Math.max(0, value.etaSeconds!) : 0,
    suggestAlternate: value.suggestAlternate === true,
  };
}

function progressPercent(progress: UpdateProgress, fallbackTotal: number): number {
  const total = progress.total > 0 ? progress.total : fallbackTotal;
  if (progress.phase === "verifying" || progress.phase === "ready") return 100;
  if (total <= 0) return 0;
  return Math.min(100, Math.round((progress.received / total) * 100));
}

export function UpdateBanner({ info, platform, onDismiss }: { info: UpdateInfo; platform: "darwin" | "windows" | "linux"; onDismiss?: () => void }) {
  const t = useT();
  const [progress, setProgress] = useState<UpdateProgress | null>(null);
  const [actionBusy, setActionBusy] = useState(false);
  const [retryInstall, setRetryInstall] = useState(false);
  const [draftFlushBlocked, setDraftFlushBlocked] = useState(false);
  const [draftStorageBlocked, setDraftStorageBlocked] = useState(false);
  const updateVersion = info.latest;

  const setCurrentProgress = useCallback((next: UpdateProgress | null) => {
    const normalized = normalizeProgress(next, updateVersion);
    if (!normalized) return;
    setProgress((current) => current?.phase !== "idle" && normalized.phase === "idle" ? current : normalized);
  }, [updateVersion]);

  const refreshStatus = useCallback(async () => {
    try {
      setCurrentProgress(await app.GetUpdateStatus());
    } catch {
      // Older shells may not have the optional updater status binding yet.
    }
  }, [setCurrentProgress]);

  useEffect(() => {
    setProgress(null);
    const off = onUpdaterProgress((next) => setCurrentProgress(next));
    void refreshStatus();
    return off;
  }, [refreshStatus, setCurrentProgress]);

  const phase = progress?.phase ?? "idle";
  const running = ACTIVE_PHASES.includes(phase);
  const cancellable = canCancelUpdateDownload(phase);
  const percent = progress ? progressPercent(progress, info.assetSize) : 0;
  const selfUpdate = platform === "windows" && (progress?.canSelfUpdate ?? info.canSelfUpdate);
  const canDownload = info.canDownload === true;
  const sources = (progress?.sources ?? info.sources ?? []).filter((source) => source === "mac" || source === "github");
  const changeSource = async (source: string) => {
    if (running || actionBusy) return;
    setActionBusy(true);
    try {
      await app.SetUpdateSource(source);
      await refreshStatus();
    } catch (error) {
      setCurrentProgress({ phase: "error", version: updateVersion, received: progress?.received ?? 0, total: progress?.total ?? info.assetSize, err: errorText(error) });
    } finally { setActionBusy(false); }
  };
  const statusLabel = useMemo(() => {
    switch (phase) {
      case "downloading": return t("update.downloading");
      case "verifying": return t("update.verifying");
      case "ready": return t("update.ready");
      case "cancelled": return t("update.cancelled");
      case "applying": return t("update.applying");
      case "error": return t("update.error");
      default: return t("update.availableShort");
    }
  }, [phase, t]);

  const download = async () => {
    if (actionBusy || running || phase === "ready") return;
    setRetryInstall(false);
    setActionBusy(true);
    try {
      await app.DownloadUpdate();
      await refreshStatus();
    } catch (error) {
      if (isActiveTaskRefusal(error)) {
        await refreshStatus();
        return;
      }
      setCurrentProgress({ phase: "error", received: progress?.received ?? 0, total: progress?.total ?? info.assetSize, err: errorText(error), version: updateVersion });
    } finally {
      setActionBusy(false);
    }
  };

  const cancel = async () => {
    if (actionBusy || !running) return;
    setRetryInstall(false);
    setActionBusy(true);
    try {
      await app.CancelUpdateDownload();
      await refreshStatus();
    } catch (error) {
      setCurrentProgress({ phase: "error", received: progress?.received ?? 0, total: progress?.total ?? info.assetSize, err: errorText(error), version: updateVersion });
    } finally {
      setActionBusy(false);
    }
  };

  const install = async () => {
    if (actionBusy || (phase !== "ready" && !(phase === "error" && retryInstall))) return;
    setRetryInstall(false);
    setDraftFlushBlocked(false);
    setDraftStorageBlocked(false);
    setActionBusy(true);
    let draftFlushFailed = false;
    try {
      await runReadyUpdateAction({
        selfUpdate,
        flush: async () => {
          try {
            await flushComposerDrafts();
          } catch (error) {
            draftFlushFailed = true;
            throw error;
          }
        },
        apply: () => app.ApplyUpdate(),
        openDownloadFolder: () => app.OpenDownloadedUpdate(),
      });
    } catch (error) {
      const blockedByDrafts = draftFlushFailed && isComposerDraftFlushError(error);
      setDraftFlushBlocked(blockedByDrafts);
      setDraftStorageBlocked(draftFlushFailed && !blockedByDrafts);
      setRetryInstall(draftFlushFailed || isActiveTaskRefusal(error));
      setCurrentProgress({ phase: "error", received: progress?.received ?? 0, total: progress?.total ?? info.assetSize, err: errorText(error), version: updateVersion });
    } finally {
      setActionBusy(false);
    }
  };

  const openDownloadPage = () => {
    if (info.downloadUrl) openExternal(info.downloadUrl);
    else void app.OpenDownloadPage();
  };

  const message = draftFlushBlocked
    ? t("update.draftsNotReady")
    : draftStorageBlocked
      ? t("update.draftSaveFailed")
      : progress?.err || (phase === "error" ? t("update.errorDetail") : statusLabel);
  return (
    <aside className="orca-update-banner" tabIndex={-1} aria-label={t("update.title")} aria-live="polite">
      <div className="orca-update-banner__icon" aria-hidden="true">
        {phase === "ready" ? <CheckCircle2 size={18} /> : running ? <RefreshCw size={18} className="orca-update-banner__spin" /> : <Download size={18} />}
      </div>
      <div className="orca-update-banner__body">
        <div className="orca-update-banner__heading">
          <strong>{t("update.available", { version: updateVersion })}</strong>
          {(progress?.source || info.source) && <span className="orca-update-banner__source">{(progress?.source || info.source) === "mac" ? t("update.source.mac") : "GitHub"}</span>}
        </div>
        <div className="orca-update-banner__status">{message}</div>
        {canDownload && phase !== "ready" && (
          <div className="orca-update-banner__transfer">
            {sources.length > 1 && <select aria-label={t("update.source")} value={progress?.source || info.source || sources[0]} disabled={running || actionBusy} onChange={(event) => void changeSource(event.target.value)}>
              {sources.map((source) => <option key={source} value={source}>{source === "mac" ? t("update.source.mac") : "GitHub"}</option>)}
            </select>}
            {phase === "downloading" && (progress?.speedBps ?? 0) > 0 && <span>{(progress!.speedBps! / 1024).toFixed(0)} KiB/s{(progress?.etaSeconds ?? 0) > 0 ? ` · ${t("update.remaining", { n: Math.ceil(progress!.etaSeconds! / 60) })}` : ""}</span>}
          </div>
        )}
        {phase === "downloading" && progress?.suggestAlternate && sources.length > 1 && <div className="orca-update-banner__status">{t("update.slowSource")}</div>}
        {progress && (running || phase === "ready" || phase === "error" || phase === "cancelled") && (
          <div className="orca-update-banner__progress-row">
            <div className="orca-update-banner__progress" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}>
              <span style={{ width: `${percent}%` }} />
            </div>
            <span className="orca-update-banner__percent">{percent}%</span>
          </div>
        )}
      </div>
      <div className="orca-update-banner__actions">
        {phase === "ready" ? (
          <button type="button" className="btn btn--small btn--primary" onClick={() => void install()} disabled={actionBusy}>
            {selfUpdate ? <PackageCheck size={14} /> : <FolderOpen size={14} />}
            {selfUpdate ? t("update.exitAndInstall") : t("update.openDownloadFolder")}
          </button>
        ) : canDownload ? (
          running && cancellable ? (
            <button type="button" className="btn btn--small" onClick={() => void cancel()} disabled={actionBusy}>
              <X size={14} />{t("update.cancel")}
            </button>
          ) : running ? (
            <button type="button" className="btn btn--small btn--primary" disabled>
              <RefreshCw size={14} className="orca-update-banner__spin" />{t("update.applying")}
            </button>
          ) : (
            <button type="button" className="btn btn--small btn--primary" onClick={() => void (retryInstall ? install() : download())} disabled={actionBusy}>
              <Download size={14} />{phase === "error" || phase === "cancelled" ? t("update.retry") : t("update.downloadNow")}
            </button>
          )
        ) : (
          <button type="button" className="btn btn--small" onClick={openDownloadPage}>
            <ExternalLink size={14} />{t("update.openDownloadPage")}
          </button>
        )}
        {onDismiss && (
          <button type="button" className="btn btn--small" onClick={onDismiss} title={t("update.later")} aria-label={t("update.later")}>
            <Clock3 size={14} />{t("update.later")}
          </button>
        )}
      </div>
    </aside>
  );
}
