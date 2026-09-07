import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { sameComposerDraft, submitComposerDraft } from "../components/Composer";

let passed = 0;
let failed = 0;

function check(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const composerSource = readFileSync(fileURLToPath(new URL("../components/Composer.tsx", import.meta.url)), "utf8");
const appSource = readFileSync(fileURLToPath(new URL("../App.tsx", import.meta.url)), "utf8");
const controllerSource = readFileSync(fileURLToPath(new URL("../lib/useController.ts", import.meta.url)), "utf8");

const baseDraft = {
  tabId: "tab-a",
  text: "keep this draft",
  attachments: [],
  workspaceRefs: [],
  sessionRefs: [],
};
const changedText = { ...baseDraft, text: "new draft" };
const changedAttachment = { ...baseDraft, attachments: [{}] };
const changedTab = { ...baseDraft, tabId: "tab-b" };
check(sameComposerDraft(baseDraft, baseDraft), "unchanged draft can be cleared after success");
check(!sameComposerDraft(baseDraft, changedText), "text entered while sending is preserved");
check(!sameComposerDraft(baseDraft, changedAttachment), "attachments added while sending are preserved");
check(!sameComposerDraft(baseDraft, changedTab), "switching tabs prevents the old send from clearing the new draft");

const draft = {
  text: "keep this draft",
  attachments: [".orca/attachments/keep.png"],
};
let clearCalls = 0;
let reported: unknown;

const sent = await submitComposerDraft(
  async () => {
    throw new Error("workspace is still starting");
  },
  () => {
    clearCalls += 1;
    draft.text = "";
    draft.attachments = [];
  },
  (error) => {
    reported = error;
  },
);

console.log("\nComposer send failure");
check(sent === false, "rejecting backend reports an unsuccessful send");
check(clearCalls === 0, "rejecting backend does not clear the draft");
check(draft.text === "keep this draft" && draft.attachments.length === 1, "text and attachments remain available for retry");
check(reported instanceof Error && reported.message === "workspace is still starting", "failure is delivered to the inline error reporter");
check(composerSource.includes('role="status"') && composerSource.includes('t("msg.sendFailed")'), "Composer renders send failures as inline status feedback");
check(composerSource.includes("if (!sameComposerDraft(submittedDraft, draftSnapshotRef.current)) return;"), "successful sends clear only their unchanged draft snapshot");
check(composerSource.includes("disabled={hasDraftContent ? (disabled || submitting") && !composerSource.includes("readOnly={submitting}"), "sending disables the send control without making the textarea readonly");
check(appSource.includes("await send(trimmed, submitText.trim())"), "App awaits the Submit callback before Composer can clear the draft");
check(controllerSource.includes("throw error;"), "controller send propagates the rejecting backend promise");
check(controllerSource.includes('if (!active?.id) throw new Error("Cannot send: no active tab is available.");'), "missing active tabs reject instead of reporting a successful send");
check(/className="composer__btn composer__btn--send"\s+type="button"\s+aria-label=\{t\("composer.send"\)\}/.test(composerSource), "idle send has an accessible name in native WebView2");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
