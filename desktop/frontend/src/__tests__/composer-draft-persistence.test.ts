import { clearComposerDraft, flushComposerDrafts, loadComposerDraft, persistComposerDraft, registerComposerDraftFlusher } from "../lib/composerDraftPersistence";

class MemoryStorage {
  private values = new Map<string, string>();
  getItem(key: string) { return this.values.get(key) ?? null; }
  setItem(key: string, value: string) { this.values.set(key, value); }
  removeItem(key: string) { this.values.delete(key); }
}

class ThrowingStorage {
  getItem() { throw new Error("storage unavailable"); }
  setItem() { throw new Error("storage unavailable"); }
  removeItem() { throw new Error("storage unavailable"); }
}

class QuotaStorage {
  readonly error = Object.assign(new Error("quota exceeded"), { name: "QuotaExceededError" });
  getItem() { throw this.error; }
  setItem() { throw this.error; }
  removeItem() { throw this.error; }
}

let passed = 0;
let failed = 0;
function check(value: boolean, label: string) {
  if (value) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}

const previousStorage = (globalThis as { localStorage?: Storage }).localStorage;
const storage = new MemoryStorage();
Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });

persistComposerDraft({
  tabId: "tab-a",
  text: "draft A",
  attachments: [
    { id: "ready-a", path: "C:/orca/ready.png", displayName: "ready.png", status: "ready", previewUrl: "data:image/png;base64,should-not-persist" },
    { id: "pending-a", path: "C:/orca/pending.zip", status: "pending" },
    { id: "failed-a", path: "C:/orca/failed.txt", status: "failed", error: "upload failed" },
  ],
  workspaceRefs: [{ path: "C:/orca/project", isDir: true }],
  sessionRefs: [{ path: "C:/orca/session.json", title: "Referenced session" }],
});

const restoredA = loadComposerDraft("tab-a");
check(restoredA?.attachments.length === 1 && restoredA.attachments[0]?.id === "ready-a", "only ready attachment metadata is restored");
check(restoredA?.attachments[0]?.displayName === "ready.png" && !JSON.stringify(restoredA).includes("should-not-persist"), "attachment previews are excluded from persisted drafts");
check(restoredA?.workspaceRefs[0]?.path === "C:/orca/project" && restoredA.sessionRefs[0]?.title === "Referenced session", "workspace and session references are preserved");

persistComposerDraft({ tabId: "tab-b", text: "draft B", attachments: [], workspaceRefs: [], sessionRefs: [] });
check(loadComposerDraft("tab-a")?.text === "draft A" && loadComposerDraft("tab-b")?.text === "draft B", "draft restoration remains isolated by tab");

storage.setItem("orca.composer-draft.v1:legacy", JSON.stringify({ text: "legacy", workspaceRefs: [], sessionRefs: [] }));
check(loadComposerDraft("legacy")?.attachments.length === 0, "old drafts without attachment metadata remain readable");

Object.defineProperty(globalThis, "localStorage", { configurable: true, value: new ThrowingStorage() });
let storageFailureHandled = true;
try {
  loadComposerDraft("tab-a");
  persistComposerDraft({ tabId: "tab-a", text: "draft", attachments: [], workspaceRefs: [], sessionRefs: [] });
  clearComposerDraft("tab-a");
} catch {
  storageFailureHandled = false;
}
check(storageFailureHandled, "storage failures never escape draft persistence helpers");

const quotaStorage = new QuotaStorage();
Object.defineProperty(globalThis, "localStorage", { configurable: true, value: quotaStorage });
let ordinaryQuotaHandled = true;
try {
  persistComposerDraft({ tabId: "tab-a", text: "best effort", attachments: [], workspaceRefs: [], sessionRefs: [] });
} catch {
  ordinaryQuotaHandled = false;
}
check(ordinaryQuotaHandled, "ordinary persistence still treats quota failures as best effort");
let strictError: unknown;
try {
  persistComposerDraft({ tabId: "tab-a", text: "must save", attachments: [], workspaceRefs: [], sessionRefs: [] }, { strict: true });
} catch (error) {
  strictError = error;
}
check(strictError === quotaStorage.error, "strict persistence rethrows the original quota error");

const unregister = registerComposerDraftFlusher(() => {
  persistComposerDraft({ tabId: "tab-a", text: "must flush", attachments: [], workspaceRefs: [], sessionRefs: [] }, { strict: true });
});
let flushError: unknown;
try {
  await flushComposerDrafts();
} catch (error) {
  flushError = error;
}
unregister();
check(flushError === quotaStorage.error, "the explicit pre-install flush remains strict");

Object.defineProperty(globalThis, "localStorage", { configurable: true, value: previousStorage });

console.log(`\ncomposer draft persistence: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
