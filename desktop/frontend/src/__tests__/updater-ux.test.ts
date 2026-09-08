import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { acceptUpdate, canCancelUpdateDownload, dismissUpdate, reopenUpdate, runReadyUpdateAction, shouldShowUpdate } from "../lib/updaterBannerState";

const root = fileURLToPath(new URL("..", import.meta.url));
const app = readFileSync(join(root, "App.tsx"), "utf8");
const banner = readFileSync(join(root, "components", "UpdateBanner.tsx"), "utf8");
const composerSource = readFileSync(join(root, "components", "Composer.tsx"), "utf8");
const bridge = readFileSync(join(root, "lib", "bridge.ts"), "utf8");
const types = readFileSync(join(root, "lib", "types.ts"), "utf8");
const en = readFileSync(join(root, "locales", "en.ts"), "utf8");
const zh = readFileSync(join(root, "locales", "zh.ts"), "utf8");

let passed = 0;
let failed = 0;
function check(value: boolean, label: string) {
  if (value) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}

console.log("\nupdater UX contract");
check(["DownloadUpdate", "CancelUpdateDownload", "GetUpdateStatus", "ApplyUpdate", "OpenDownloadedUpdate"].every((name) => bridge.includes(`${name}()`)), "bridge exposes the native updater actions");
check(bridge.includes('"updater:progress"') && bridge.includes("onUpdaterProgress"), "bridge subscribes to updater progress");
check(types.includes("canDownload: boolean") && types.includes("source?: string") && types.includes("UpdateProgressPhase"), "frontend types cover download capability and progress phases");
check(banner.includes("app.DownloadUpdate()") && banner.includes("app.CancelUpdateDownload()") && banner.includes("app.GetUpdateStatus()"), "banner downloads, cancels, and refreshes status");
check(banner.includes("isActiveTaskRefusal") && banner.includes("await refreshStatus()"), "active-task refusal re-reads status");
check(banner.includes("flushComposerDrafts()") && banner.includes("app.ApplyUpdate()") && banner.includes("isComposerDraftFlushError") && banner.includes("draftSaveFailed") && composerSource.includes("strict: true"), "self-install strictly flushes drafts before applying");
check(banner.includes('t("update.exitAndInstall")') && banner.includes('t("update.openDownloadFolder")'), "ready state uses the platform-specific install action");
check(banner.includes('platform === "windows"') && banner.includes("canSelfUpdate"), "self-install is limited to Windows capability reports");
check(banner.includes("finally") && banner.includes("setActionBusy(false)"), "ready actions always clear the busy state");
check(banner.includes("canCancelUpdateDownload") && !canCancelUpdateDownload("applying") && canCancelUpdateDownload("downloading") && canCancelUpdateDownload("verifying"), "applying cannot enter the download cancellation action");
check(app.includes("!updateInfo.canDownload") && app.includes("orca-update-banner"), "App routes downloads through the in-app updater and keeps the webpage as fallback");
check(app.includes("shouldShowUpdate") && app.includes("dismissUpdate") && app.includes("reopenUpdate"), "later dismissal is memory-only and can be reopened");
check(!banner.includes("app.Cancel()") && !banner.includes("app.Stop"), "updater UX never manually stops agent tasks");
check(["update.downloadNow", "update.cancel", "update.retry", "update.exitAndInstall", "update.openDownloadFolder", "update.draftsNotReady", "update.draftSaveFailed", "update.later", "update.availableShort"].every((key) => en.includes(`"${key}"`) && zh.includes(`"${key}"`)), "download controls are localized in English and Chinese");
check(!banner.includes("requestComposerDraftFlush") && !banner.includes("update.openInstaller"), "banner uses the current draft flush and folder-opening contracts");

let opened = 0;
let applied = 0;
let flushed = 0;
await runReadyUpdateAction({
  selfUpdate: false,
  flush: async () => { flushed += 1; },
  apply: async () => { applied += 1; },
  openDownloadFolder: async () => { opened += 1; },
});
check(opened === 1 && applied === 0 && flushed === 0, "opening the download folder does not flush, apply, or cancel a task");

let openFailure: unknown;
try {
  await runReadyUpdateAction({
    selfUpdate: false,
    flush: async () => {},
    apply: async () => {},
    openDownloadFolder: async () => { throw new Error("folder unavailable"); },
  });
} catch (error) {
  openFailure = error;
}
check(openFailure instanceof Error && openFailure.message === "folder unavailable", "download-folder failures remain observable for action cleanup");

const initialDismissal = acceptUpdate({ version: "", dismissed: false }, "3.0.3");
const laterDismissal = dismissUpdate(initialDismissal, "3.0.3");
check(!shouldShowUpdate(laterDismissal, "3.0.3") && shouldShowUpdate(laterDismissal, "3.0.4"), "later hides only the current update and new versions reopen it");
check(shouldShowUpdate(reopenUpdate(laterDismissal, "3.0.3"), "3.0.3"), "the update entry can reopen a dismissed banner without changing download status");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
