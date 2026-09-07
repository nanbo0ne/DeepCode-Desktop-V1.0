import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const css = readFileSync(join(root, "styles.css"), "utf8");
const chrome = readFileSync(join(root, "components", "AppChrome.tsx"), "utf8");
const app = readFileSync(join(root, "App.tsx"), "utf8");
const composer = readFileSync(join(root, "components", "Composer.tsx"), "utf8");
const settings = readFileSync(join(root, "components", "SettingsPanel.tsx"), "utf8");
const processCard = readFileSync(join(root, "components", "ProcessCard.tsx"), "utf8");
const transcript = readFileSync(join(root, "components", "Transcript.tsx"), "utf8");
const todoPanel = readFileSync(join(root, "components", "TodoPanel.tsx"), "utf8");
const promptShelf = readFileSync(join(root, "components", "PromptShelf.tsx"), "utf8");
const toolCard = readFileSync(join(root, "components", "ToolCard.tsx"), "utf8");
const controller = readFileSync(join(root, "lib", "useController.ts"), "utf8");

let passed = 0;
let failed = 0;
function check(value: boolean, label: string) {
  if (value) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}

console.log("\nresponsive layout safeguards");
check(!css.includes("padding: 5px 120px"), "composer has no fixed right-side reservation");
check(!css.includes(".composer-card__actions {\n    right:"), "composer actions stay in grid flow");
check(chrome.includes("function ClassicAppChrome") && chrome.includes("function ModernAppChrome"), "chrome uses explicit presentation branches");
check(
  app.includes('{desktopUIStyle === "classic" && (\n          <header className="topicbar">'),
  "modern chrome does not stack the legacy TopicBar as a third header row",
);
check(css.includes("@media (max-width: 1100px)") && css.includes("@media (max-width: 780px)"), "chrome priorities have narrow breakpoints");
check(
  ["modernMenu.file", "modernMenu.project", "modernMenu.tools", "modernMenu.settings"].every((key) => chrome.includes(`t("${key}")`)) &&
    chrome.includes("onOpenSkills") && chrome.includes("onOpenBots") && chrome.includes("onOpenAutomations") && chrome.includes("onOpenToolLibrary"),
  "modern text menus keep all secondary commands available",
);
check(
  css.includes(':root[data-ui-style="modern"] .modern-chrome__workspace-actions') &&
    css.includes(':root[data-ui-style="modern"] .modern-chrome__icon-button'),
  "modern workspace actions keep stable icon slots",
);
check(app.includes("topicbar__overflow-menu") && css.includes(".topicbar__action--direct-utility"), "topic actions expose a narrow overflow menu");
check(css.includes(".statusbar {\n    max-width: none;\n    gap: 6px;\n    overflow: hidden"), "narrow status bar cannot wrap or overflow");
check(
  css.includes(".footer-shelves > .todobar {\n  flex: 0 0 auto;\n  width: min(520px, 100%);") &&
    css.includes("min-width: min(300px, 100%);\n  max-width: 100%;\n  margin-inline: auto;"),
  "Todo stays centered and constrained by the conversation footer",
);
check(
  settings.includes('<SettingsField label={t("settings.autoCheckUpdates")}') &&
    settings.includes('<div className="settings-update-control">') &&
    !settings.includes('<SettingsField label={t("settings.checkUpdatesNow")}'),
  "manual update check stays beside the automatic update toggle",
);
check(
  app.includes("app.GetProductCapabilities()") &&
    composer.includes("promptModes.map((mode)") &&
    !composer.includes("PROMPT_MODE_OPTIONS") &&
    !composer.includes("CircleHelp") &&
    !composer.includes("promptModeHelpKey"),
  "prompt modes come from product capabilities without descriptions or a help button",
);
check(
  app.includes("if (!activeTabId || state.running) return;") &&
    app.includes("pendingPromptModesByTab, activeTabId") &&
    app.includes("void applyPendingRuntimePrefs(activeTabId);") &&
    app.includes("pendingPromptModeSwitchRef.current[activeTabId]"),
  "a confirmed running mode switch applies as soon as the turn becomes idle",
);
check(
  app.includes("promptModeSwitchFailedByTab") &&
    app.includes("promptModeSwitchFailed || state.approval") &&
    app.includes("setPromptModeSwitchFailedByTab((current) => ({ ...current, [tabId]: true }))") &&
    app.indexOf("submitPromptToAgent(nextPrompt.displayText, nextPrompt.submitText).then") <
      app.indexOf("const rest = queue.slice(1)", app.indexOf("submitPromptToAgent(nextPrompt.displayText, nextPrompt.submitText).then")),
  "failed mode switches retain queued prompts until a successful retry",
);
check(
  !settings.includes('<SettingsField label={t("settings.botModel")}') &&
    !settings.includes('<SettingsField label={t("settings.botPromptMode")}') &&
    !settings.includes('<SettingsField label={t("settings.botWorkspaceRoot")}') &&
    settings.includes("productCapabilities.automationWorkspaceEnabled === true"),
  "bot and memory settings follow the product capability boundary",
);
check(
  app.includes("promptModes={automationConversation ? [] : productCapabilities.promptModes}") &&
    app.includes("promptModeLocked={false}") &&
    app.includes("showToolApprovalControls={!automationConversation}") &&
    composer.includes("showToolApprovalControls && uiStyle === \"modern\"") &&
    composer.includes("showToolApprovalControls && uiStyle === \"classic\"") &&
    composer.includes("disabled={disabled || promptModeLocked}"),
  "Orca hides both the ordinary mode selector and redundant approval selector",
);
const chooser = readFileSync(join(root, "components", "NewSessionChooser.tsx"), "utf8");
const projectTree = readFileSync(join(root, "components", "ProjectTree.tsx"), "utf8").replace(/\r\n/g, "\n");
check(
  !chooser.includes('choose("automation"') &&
    projectTree.includes('node.kind === "orca_topic"') &&
    !projectTree.includes('node.kind === "automation_folder"') &&
    !projectTree.includes("automation_history_folder"),
  "Orca is a fixed top-level entry rather than a creatable workspace",
);
check(
  projectTree.includes('node.kind === "orca_topic" ? "automation"') &&
    projectTree.includes("node.readOnly || node.primary ? []"),
  "Orca stays fixed instead of inheriting project rename and drag actions",
);
const composerContract = css.slice(css.indexOf("/* Composer responsive contract."));
check(
  [720, 580, 460, 380, 320].every((width, index, widths) => {
    const position = composerContract.indexOf(`@container (max-width: ${width}px)`);
    const previous = index === 0 ? -1 : composerContract.indexOf(`@container (max-width: ${widths[index - 1]}px)`);
    return position > previous;
  }),
  "final Composer breakpoints use one descending 720/580/460/380/320 sequence",
);
check(
  composerContract.includes(".composer-meta__control--model {\n    display: inline-flex;") &&
    composerContract.includes(".composer-enhanced__button svg:last-child {\n    display: none;"),
  "narrow Composer retains model access and collapses mode trigger to one icon",
);
check(
  css.includes(':root[data-ui-style="modern"] .composer-card {\n  grid-template-columns: minmax(0, 1fr) max-content;') &&
    css.includes("max-width: min(360px, 42cqw);") &&
    css.includes("@container (max-width: 320px)") &&
    css.includes(':root[data-ui-style="modern"] .composer-card__actions--modern {\n  display: flex;') &&
    css.includes("width: max-content;\n  max-width: min(100%, calc(100cqw - 88px));\n  min-width: 0;\n  justify-self: end;"),
  "Modern footer uses a content-sized right-aligned action track",
);
check(
  css.includes(':root[data-ui-style="modern"] .transcript {\n  width: 100%;\n  max-width: none;') &&
    !css.includes(':root[data-ui-style="modern"] .main__scroll,\n:root[data-ui-style="modern"] .transcript,'),
  "Modern transcript keeps a full-width scroll viewport around the reading column",
);
check(
  css.includes("width: auto;\n  max-width: min(280px, 34cqw);") &&
    css.includes("max-width: 148px;\n  flex: 0 1 auto;"),
  "Modern run status uses intrinsic width so controls remain grouped at the right edge",
);
check(
    composer.includes('composer-runstatus__primary--${hasDraftContent ? "send" : "stop"}') &&
    composer.includes("onClick={hasDraftContent ? () => void submit() : handleCancel}") &&
    composer.includes("hasDraftContent ? (disabled || submitting || pendingPaste > 0 || !hasSendableContent) : cancelRequested") &&
    composer.includes('hasDraftContent ? <ArrowUp size={13} /> : <Square size={11} fill="currentColor" strokeWidth={1.8} />') &&
    css.includes(".composer-runstatus__primary {\n  --wails-draggable: no-drag;") &&
    css.includes("width: 34px;\n  min-width: 34px;\n  max-width: 34px;\n  height: 34px;") &&
    css.includes(".composer-runstatus__primary--send {") &&
    composerContract.includes(".composer-runstatus__primary {\n    width: 30px;\n    min-width: 30px;\n    max-width: 30px;") &&
    !composer.includes("composer-runstatus__primary-label"),
  "running send replaces stop without changing the primary action geometry",
);
check(
  composer.includes("Plain text always follows the textarea's native paste path") &&
    composer.includes('if (pasted !== "") return;') &&
    !composer.includes("shouldFoldPaste") &&
    !composer.includes("composer__pasted"),
  "plain text paste remains editable text and wins over rich clipboard image hints",
);
check(
  app.includes('target.classList.contains("composer__input")') &&
    app.includes("pasteRequest={composerPasteRequest}") &&
    composer.includes("pasteFromContextMenu") &&
    composer.includes("await attachNativeClipboardImage(true)"),
  "custom context-menu paste delegates images and files to Composer",
);
check(
  css.includes(":root[data-theme-style] .composer__btn--send:disabled") &&
    css.includes("background: var(--control-disabled-bg)") &&
    css.includes("color: var(--control-disabled-fg)"),
  "themed disabled send button stays muted",
);
check(
  composer.includes("!hasSendableContent && !(goalModeOn && !activeGoal)") &&
    !composer.includes("!text.trim() && attachments.length === 0 && workspaceRefs.length === 0"),
  "failed attachments cannot leave the ordinary send button falsely enabled",
);
check(
  composer.includes("const COMPOSER_AUTO_MAX_LINES = 10") &&
    composer.includes("composerAutoInputMaxHeight(node)") &&
    composer.includes("node.scrollHeight > maxHeight + 1") &&
    css.includes(".composer__input {\n  flex: 1;\n  resize: none;\n  margin: 0;\n  padding: 0;"),
  "Composer grows with content up to a stable line limit before scrolling",
);
check(
  settings.includes('["compact", "detailed"]') &&
    !settings.includes('["compact", "standard", "detailed"]'),
  "process settings expose only compact and detailed modes",
);
check(
  processCard.includes("const closeFromKeyboard") &&
    processCard.includes("{hasBody && actualOpen && (") &&
    transcript.includes("processOpenOverrides.get(segment.id)") &&
    transcript.includes("next.set(segment.id, nextOpen)"),
  "compact process details can close cleanly and preserve stable segment overrides",
);
check(
  css.includes(".process-activity-mark") &&
    css.includes("prefers-reduced-motion: reduce") &&
    css.includes("process-activity-spinner--tool") &&
    css.includes("process-activity-spin-clockwise") &&
    css.includes("process-activity-spin-counterclockwise") &&
    !css.includes("animation-direction") &&
    transcript.includes("activityIndicatorPhase(items, activityIndicatorEnabled, running, paused)") &&
    transcript.includes("timeline-entry--activity"),
  "single activity ring regenerates for each direction and remains reduced-motion safe",
);
check(
  !css.includes(".composer-enhanced__button--switching svg:first-child") &&
    composer.includes("<RuntimeSwitchBar progress={runtimeSwitch} />") &&
    css.includes("width: min(180px, calc(100% - 16px));") &&
    css.includes("height: 3px;"),
  "mode switching keeps the selector still and shows a right-aligned stage bar",
);
check(
  composer.includes("<Pause size={15}") && composer.includes("<Play size={14}") &&
    !composer.includes("PauseCircle") && !composer.includes("PlayCircle") &&
    composer.includes('<Square size={11} fill="currentColor"'),
  "pause, resume, and stop share a consistent plain-icon language",
);
check(
  transcript.includes('case "mode_switch":') && transcript.includes("<ModeSwitchRow item={segment.item} />") &&
    controller.includes('kind: "mode_switch"') && controller.includes('type: "runtime_switch"'),
  "runtime mode switches render as standalone persistent timeline rows",
);
check(
  css.includes(".footer {\n  position: relative;") &&
    css.includes("border-top: 0;\n  background: transparent;") &&
    css.includes(".footer-shelves") &&
    css.includes("background: transparent;"),
  "footer shelves remain transparent outside their individual cards",
);
check(
  todoPanel.includes("AnchoredPopover") &&
    todoPanel.includes('className="todobar__surface"') &&
    todoPanel.includes('data-ui-surface="panel"') &&
    todoPanel.includes('className="todobar__details"') &&
    todoPanel.includes('placement="auto"') &&
    css.includes(".todo-popover"),
  "Todo keeps a stable in-flow trigger and opens details in an anchored portal",
);
check(
  promptShelf.includes('data-ui-surface="panel"') &&
    transcript.includes('data-ui-surface="panel"') &&
    !transcript.includes('className={`turn-stats-row turn-process-panel${open ? " turn-stats-row--open" : ""}`} data-ui-surface="panel"') &&
    !processCard.includes('data-ui-surface="panel"') &&
    !toolCard.includes('data-ui-surface="panel"'),
  "panel ownership stays on the shelf or turn while process and tool rows remain flat",
);
check(
  css.includes(".turn-process-panel {\n  width: min(100%, 820px);\n  overflow: visible;\n  border: 0;\n  border-radius: 0;\n  background: transparent;") &&
    !css.includes(".turn-process-panel > button"),
  "completed-turn summary uses the original transparent inline treatment",
);
check(
  toolCard.includes('data-ui-surface="content"') &&
    css.includes(":root[data-theme-style] .process-activity-rail .process-card__body {\n  padding: 0 4px 8px 25px;\n  border-top: 0;\n  background: transparent;"),
  "only tool output surfaces retain contained backgrounds inside the activity rail",
);
check(
  transcript.includes("transcript--hydrating") &&
    css.includes(".transcript--hydrating .timeline-entry") &&
    css.includes("animation: none !important;"),
  "restored history skips bulk entrance animation during its first paint",
);
check(
  controller.includes("Meta and history are the only first-paint dependencies") &&
    controller.includes("afterNextPaint()") &&
    controller.includes('dispatchTo(meta.id, { type: "session_load_start"') &&
    controller.indexOf("safe(app.EffortForTab(tabId))") > controller.indexOf('type: "session_primary_loaded"'),
  "conversation selection paints before history work and auxiliary status hydrates later",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
