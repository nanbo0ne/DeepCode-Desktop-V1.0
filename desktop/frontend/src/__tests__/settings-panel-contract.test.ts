import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { localDownloadActions, wrappedFocusIndex } from "../lib/settingsPanelState";

let passed = 0;
let failed = 0;

function check(condition: boolean, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function same<T>(actual: T, expected: T): boolean {
  return JSON.stringify(actual) === JSON.stringify(expected);
}

console.log("\nsettings panel contracts");
check(same(localDownloadActions("downloading"), ["pause", "cancel"]), "downloading pauses or cancels");
check(same(localDownloadActions("paused"), ["resume", "cancel"]), "paused downloads can resume or cancel");
check(same(localDownloadActions("queued"), ["cancel"]), "queued downloads only offer cancel");
check(same(localDownloadActions("verifying"), ["cancel"]), "verifying downloads never offer resume");
check(same(localDownloadActions("failed"), ["retry"]), "failed downloads offer retry");
check(same(localDownloadActions("cancelled"), ["redownload"]), "cancelled downloads offer a fresh download");
check(wrappedFocusIndex(2, 3, false) === 0, "forward focus wraps to the first control");
check(wrappedFocusIndex(0, 3, true) === 2, "reverse focus wraps to the last control");
check(wrappedFocusIndex(-1, 3, false) === 0 && wrappedFocusIndex(-1, 3, true) === 2, "focus entering from outside lands at the correct edge");
check(wrappedFocusIndex(0, 0, false) === -1, "empty focus lists are handled");

const root = fileURLToPath(new URL("..", import.meta.url));
const settings = readFileSync(join(root, "components", "SettingsPanel.tsx"), "utf8");
const onboarding = readFileSync(join(root, "components", "OnboardingOverlay.tsx"), "utf8");
const en = readFileSync(join(root, "locales", "en.ts"), "utf8");
const zh = readFileSync(join(root, "locales", "zh.ts"), "utf8");
check(settings.includes("const [settingsError, setSettingsError]"), "settings load errors have dedicated state");
check(settings.includes("settingsError &&") && settings.includes("settings.retryLoad"), "settings load errors expose retry");
check(!settings.includes("app.Settings().catch(() => null)"), "settings failures are not converted to permanent null loading");
check(settings.includes('role="dialog" aria-modal="true"') && settings.includes('aria-labelledby="settings-dialog-title"'), "settings exposes dialog semantics");
check(settings.includes("element.inert = true") && settings.includes("returnFocusRef.current?.focus()"), "settings isolates and restores focus");
check(settings.includes("onPortalKeyDown") && settings.includes("data-anchored-popover='active'"), "settings keeps portal menus in the focus scope");
check(settings.includes("runLocalAction") && settings.includes("settings.localAI.operationFailed"), "local AI rejects stay inside the page");
check(!settings.includes("StartLocalModelDownload(model.id).then(reload)"), "local AI download does not use an unhandled promise chain");
check(settings.includes("if (!refreshed) setRetryAction(() => () => { void reload(); });") && !settings.includes("if (!refreshed) setRetryAction(() => () => { void runLocalAction(label, operation); });"), "successful mutations retry only catalog reloads");
check(settings.includes("catalogRef.current") && settings.includes("gpuDetectionFailed"), "local AI keeps refresh state and distinguishes GPU detection failure");
check(onboarding.includes('useT') && onboarding.includes('onboarding.deepseekPrivacyLocal'), "onboarding copy comes from i18n");
check(en.includes('"settings.tab.localAI": "Local AI"') && zh.includes('"settings.tab.localAI": "本地 AI"'), "new settings tabs exist in both locales");
check(en.includes('"settings.localAI.redownload": "Download again"') && zh.includes('"settings.localAI.redownload": "重新下载"'), "download recovery actions exist in both locales");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
