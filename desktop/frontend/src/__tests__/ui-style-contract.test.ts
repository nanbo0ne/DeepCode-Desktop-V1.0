import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const source = readFileSync(join(root, "lib", "uiStyle.ts"), "utf8");
const chrome = readFileSync(join(root, "components", "AppChrome.tsx"), "utf8");
const composer = readFileSync(join(root, "components", "Composer.tsx"), "utf8");
const app = readFileSync(join(root, "App.tsx"), "utf8");
const css = readFileSync(join(root, "styles.css"), "utf8");
if (!source.includes('root.removeAttribute("data-theme-style")')) {
  throw new Error("classic style must remove the modern overlay marker");
}
console.log("  PASS  classic style restores the baseline marker state");
if (!chrome.includes("function ClassicAppChrome") || !chrome.includes("function ModernAppChrome")) {
  throw new Error("classic and modern shells must use explicit DOM branches");
}
if (!app.includes("workspaceLabel={topicbarWorkspaceLabel") || !app.includes("sessionLabel={topicbarTitle}")) {
  throw new Error("second shell row must receive the active workspace and session labels");
}
if (!app.includes('const activeUIStyleRef = useRef<UIStyle>(getUIStyle())') || !app.includes('applyUIStyle(runtimeStyle, { persist: false })')) {
  throw new Error("preference refreshes must preserve the active window style boundary");
}
if (!chrome.includes('t("modernMenu.file")') || !chrome.includes('t("modernMenu.project")') || !chrome.includes('t("modernMenu.tools")') || !chrome.includes('t("modernMenu.settings")')) {
  throw new Error("modern shell must expose the four fixed text menus");
}
if (!chrome.includes('const [openMenu, setOpenMenu]') || !chrome.includes('document.addEventListener("pointerdown", closeOutside)')) {
  throw new Error("modern menus must be mutually exclusive and close outside");
}
if (!chrome.includes('event.key === "Escape"') || !chrome.includes('event.key === "ArrowLeft"') || !chrome.includes('event.key === "ArrowDown"')) {
  throw new Error("modern menus must implement keyboard menu navigation");
}
if (!chrome.includes('aria-label="O.R.C.A."') || !app.includes('workspaceLabel={topicbarWorkspaceLabel')) {
  throw new Error("modern workspace row must expose the O.R.C.A. identity and active workspace");
}
if (!composer.includes('composer-card__actions--modern') || !composer.includes('composer-modern-parameters') || !composer.includes('composer-modern-access')) {
  throw new Error("modern Composer must use an explicit single-row control structure");
}
if (composer.includes('uiStyle === "modern" && showToolApprovalControls && (') || composer.includes('uiStyle === "modern" && showToolApprovalControls && (')) {
  throw new Error("modern permission controls must not be rendered inside the intent menu");
}
if (!composer.includes('showToolApprovalControls && uiStyle === "classic"') || !composer.includes('composer-meta--classic')) {
  throw new Error("classic Composer must retain its legacy control branch");
}
if (!chrome.includes('className="app-chrome__window-controls app-chrome__window-controls--windows" aria-hidden="true"')) {
  throw new Error("classic browser preview must use static legacy window controls");
}
if (!css.includes(':root[data-ui-style="modern"] .modern-chrome')) {
  throw new Error("modern shell CSS must be scoped to the modern branch");
}
if (!css.includes(':root[data-ui-style="modern"] .composer-card__actions--modern') || !css.includes(':root[data-ui-style="modern"] .composer-modern-parameters')) {
  throw new Error("modern Composer controls must have an authoritative scoped layout");
}
if (!css.includes(':root[data-ui-style="classic"] .composer-meta--classic')) {
  throw new Error("classic Composer controls must have an explicit scoped baseline");
}
if (!css.includes('--app-chrome-height: 76px') || !css.includes('grid-template-rows: 32px 44px')) {
  throw new Error("modern shell must keep its two rows compact");
}
if ((css.match(/--app-chrome-height: 76px/g) || []).length !== 1 || (css.match(/grid-template-rows: 32px 44px/g) || []).length !== 1) {
  throw new Error("modern shell geometry must have one authoritative definition");
}
if (!css.includes(':root[data-ui-style="modern"] .onboarding__card') || !css.includes('width: min(500px, calc(100vw - 48px))')) {
  throw new Error("modern onboarding must use the compact scoped sheet");
}
console.log("  PASS  explicit classic and modern chrome contracts");

if (css.includes('--app-chrome-height: 46px')) {
  throw new Error("color themes must not override the presentation's header height");
}
if (!css.includes('height: var(--app-chrome-height)') || !css.includes('padding: 10px 10px calc(8px + var(--statusbar-height))')) {
  throw new Error("modern header and sidebar must reserve the visible chrome and status bar");
}
if (!css.includes('.composer-modern-parameter > .modelsw') || !css.includes('min-width: 68px')) {
  throw new Error("narrow model wrappers must shrink without squeezing the two run controls");
}
if (!chrome.includes('t("topbar.windowMinimize")') || !chrome.includes('t("topbar.windowClose")')) {
  throw new Error("window controls must follow the selected language");
}
console.log("  PASS  audited shell occupancy and narrow controls");
