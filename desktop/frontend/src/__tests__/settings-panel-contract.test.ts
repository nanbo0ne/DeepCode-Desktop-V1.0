import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { canChangeComputerUseAuthorization, localDownloadActions, wrappedFocusIndex } from "../lib/settingsPanelState";
import { app } from "../lib/bridge";
import type { ComputerUseCapabilities } from "../lib/types";

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

const legacyComputerCapabilities: ComputerUseCapabilities = {
  platform: "windows", supported: true, screenCapture: true, uiAutomation: true,
  inputInjection: true, overlay: true, emergencyStop: true,
};
const disabledComputerCapabilities: ComputerUseCapabilities = {
  ...legacyComputerCapabilities, supported: false, temporarilyDisabled: true,
};
check(!canChangeComputerUseAuthorization(disabledComputerCapabilities, false), "temporary disable blocks new authorization");
check(canChangeComputerUseAuthorization(disabledComputerCapabilities, true), "saved authorization remains revocable while temporarily disabled");
check(!canChangeComputerUseAuthorization({ ...disabledComputerCapabilities, supported: true }, false), "temporary disable takes precedence over a conflicting supported flag");
check(canChangeComputerUseAuthorization(legacyComputerCapabilities, false), "legacy supported capabilities without the new flag retain their behavior");
check(!canChangeComputerUseAuthorization({ supported: false }, false) && canChangeComputerUseAuthorization({ supported: false }, true), "unsupported platforms block new grants but allow revocation");
check(!canChangeComputerUseAuthorization(null, false) && canChangeComputerUseAuthorization(undefined, true), "loading or missing capabilities cannot authorize or prevent revoking a saved grant");

const initialSettings = await app.Settings();
const initialComputerState = await app.GetComputerUseState();
check(initialComputerState.capabilities.temporarilyDisabled === true && initialComputerState.capabilities.supported === false, "browser mock reports the release disable flag");
check(initialComputerState.approved === initialSettings.computerUseFullAccessApproved && initialComputerState.modelRef === initialSettings.computerControlModel, "reading disabled capabilities preserves saved approval and model configuration");
const savedControlModel = "saved-provider/saved-control-model";
await app.SetComputerControlModel(savedControlModel);
let authorizationError: unknown;
try {
  await app.SetComputerUseFullAccess(true);
} catch (error) {
  authorizationError = error;
}
const afterGrantAttempt = await app.Settings();
check(authorizationError instanceof Error && afterGrantAttempt.computerUseFullAccessApproved === initialSettings.computerUseFullAccessApproved, "mock rejects new authorization without changing saved approval");
check(afterGrantAttempt.computerControlModel === savedControlModel && (await app.GetComputerUseState()).capabilities.supported === false, "editing the saved model cannot enable computer use");
await app.SetComputerUseFullAccess(false);
check((await app.Settings()).computerUseFullAccessApproved === false && (await app.Settings()).computerControlModel === savedControlModel, "revocation is accepted while retaining the selected model");
await app.SetVisionModel("vision-provider/image-model");
const afterVisionChange = await app.Settings();
check(afterVisionChange.visionModel === "vision-provider/image-model" && afterVisionChange.computerControlModel === savedControlModel, "ordinary vision model editing remains independent of disabled computer use");
check(afterVisionChange.visionMode === initialSettings.visionMode && afterVisionChange.visionEnabled === initialSettings.visionEnabled && afterVisionChange.subagentModel === initialSettings.subagentModel, "computer control changes preserve vision mode and general subagent settings");
await app.SetComputerControlModel(initialSettings.computerControlModel);
await app.SetVisionModel(initialSettings.visionModel ?? "");

const root = fileURLToPath(new URL("..", import.meta.url));
const settings = readFileSync(join(root, "components", "SettingsPanel.tsx"), "utf8");
const onboarding = readFileSync(join(root, "components", "OnboardingOverlay.tsx"), "utf8");
const en = readFileSync(join(root, "locales", "en.ts"), "utf8");
const zh = readFileSync(join(root, "locales", "zh.ts"), "utf8");
const computerSection = settings.slice(settings.indexOf("function ComputerUseSection("), settings.indexOf("function AboutSection("));
check(computerSection.includes('className="banner banner--error" role="status"') && computerSection.includes('t("settings.computer.temporarilyDisabled")'), "temporary disable has a prominent localized status banner");
check(computerSection.includes("disabled={busy || !authorizationAllowed}") && computerSection.includes("if (busy || !authorizationAllowed) return;"), "authorization is guarded both at the button and the action handler");
check(computerSection.includes("s.computerControlModel,") && computerSection.includes('value={s.computerControlModel || ""}'), "saved models remain selectable even if absent from current providers");
check(computerSection.includes('disabled={busy} value={s.computerControlModel || ""}') && computerSection.includes("settings.computer.disabledModelHint"), "model editing remains enabled with an explicit disabled-release hint");
check(!computerSection.includes("SetVisionModel") && !computerSection.includes("StartComputerUseSession") && !computerSection.includes("ResumeComputerUse"), "the computer settings page cannot start a session or mutate ordinary vision settings");
check(!settings.includes('return s.computerUseFullAccessApproved ? t("settings.computer.authorized") : t("settings.computer.authorize")'), "navigation does not advertise authorization as an available action");
check(["temporarilyDisabled", "disabledModelHint", "disabledAccessHint", "authorizationUnavailable"].every((key) => en.includes(`"settings.computer.${key}"`) && zh.includes(`"settings.computer.${key}"`)), "temporary disable status and configuration hints exist in both languages");
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
