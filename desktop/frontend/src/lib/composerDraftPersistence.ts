import type { SessionReference } from "./types";

export const FLUSH_COMPOSER_DRAFTS_EVENT = "orca:flush-composer-drafts";

export class ComposerDraftFlushError extends Error {
  readonly code = "attachments-not-ready";

  constructor() {
    super("composer attachments are not ready");
    this.name = "ComposerDraftFlushError";
  }
}

export function isComposerDraftFlushError(error: unknown): error is ComposerDraftFlushError {
  return error instanceof ComposerDraftFlushError || (isRecord(error) && error.code === "attachments-not-ready");
}

export interface PersistedComposerDraft {
  text: string;
  attachments: Array<{ id: string; path: string; displayName?: string }>;
  workspaceRefs: Array<{ path: string; isDir?: boolean }>;
  sessionRefs: SessionReference[];
}

interface DraftInput {
  tabId?: string;
  text: string;
  attachments: readonly unknown[];
  workspaceRefs: readonly unknown[];
  sessionRefs: readonly unknown[];
}

export interface PersistComposerDraftOptions {
  strict?: boolean;
}

const DRAFT_KEY_PREFIX = "orca.composer-draft.v1:";

function draftKey(tabId?: string): string {
  return `${DRAFT_KEY_PREFIX}${tabId || "global"}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function loadComposerDraft(tabId?: string): PersistedComposerDraft | null {
  if (typeof localStorage === "undefined") return null;
  try {
    const raw = JSON.parse(localStorage.getItem(draftKey(tabId)) ?? "null") as Partial<PersistedComposerDraft> | null;
    if (!raw || typeof raw.text !== "string") return null;
    const attachments = Array.isArray(raw.attachments)
      ? raw.attachments.filter(isRecord).map((attachment) => ({
        id: typeof attachment.id === "string" ? attachment.id : "",
        path: typeof attachment.path === "string" ? attachment.path : "",
        ...(typeof attachment.displayName === "string" ? { displayName: attachment.displayName } : {}),
      })).filter((attachment) => attachment.id && attachment.path)
      : [];
    const workspaceRefs = Array.isArray(raw.workspaceRefs)
      ? raw.workspaceRefs.filter(isRecord).map((ref) => ({
        path: typeof ref.path === "string" ? ref.path : "",
        ...(typeof ref.isDir === "boolean" ? { isDir: ref.isDir } : {}),
      })).filter((ref) => ref.path)
      : [];
    const sessionRefs = Array.isArray(raw.sessionRefs)
      ? raw.sessionRefs.filter(isRecord).map((ref) => ({
        path: typeof ref.path === "string" ? ref.path : "",
        title: typeof ref.title === "string" ? ref.title : "",
        ...(typeof ref.preview === "string" ? { preview: ref.preview } : {}),
        ...(typeof ref.turns === "number" ? { turns: ref.turns } : {}),
        ...(typeof ref.createdAt === "number" ? { createdAt: ref.createdAt } : {}),
        ...(typeof ref.lastActivityAt === "number" ? { lastActivityAt: ref.lastActivityAt } : {}),
      })).filter((ref) => ref.path)
      : [];
    return { text: raw.text, attachments, workspaceRefs, sessionRefs };
  } catch {
    return null;
  }
}

export function persistComposerDraft(input: DraftInput, options: PersistComposerDraftOptions = {}): void {
  if (typeof localStorage === "undefined") {
    if (options.strict) throw new Error("composer draft storage unavailable");
    return;
  }
  const text = input.text;
  const attachments = input.attachments.filter(isRecord)
    .filter((attachment) => attachment.status === "ready")
    .map((attachment) => ({
      id: typeof attachment.id === "string" ? attachment.id : "",
      path: typeof attachment.path === "string" ? attachment.path : "",
      ...(typeof attachment.displayName === "string" ? { displayName: attachment.displayName } : {}),
    })).filter((attachment) => attachment.id && attachment.path);
  const workspaceRefs = input.workspaceRefs.filter(isRecord).map((ref) => ({
    path: typeof ref.path === "string" ? ref.path : "",
    ...(typeof ref.isDir === "boolean" ? { isDir: ref.isDir } : {}),
  })).filter((ref) => ref.path);
  const sessionRefs = input.sessionRefs.filter(isRecord).map((ref) => ({
    path: typeof ref.path === "string" ? ref.path : "",
    title: typeof ref.title === "string" ? ref.title : "",
  })).filter((ref) => ref.path);
  try {
    if (!text.trim() && attachments.length === 0 && workspaceRefs.length === 0 && sessionRefs.length === 0) {
      localStorage.removeItem(draftKey(input.tabId));
      return;
    }
    localStorage.setItem(draftKey(input.tabId), JSON.stringify({ text, attachments, workspaceRefs, sessionRefs } satisfies PersistedComposerDraft));
  } catch (error) {
    if (options.strict) throw error;
    // Ordinary typing persistence is best effort and must never block sending.
  }
}

export function clearComposerDraft(tabId?: string): void {
  try {
    localStorage.removeItem(draftKey(tabId));
  } catch {
    /* private mode / no storage */
  }
}

type DraftFlusher = () => void | Promise<void>;
const draftFlushers = new Set<DraftFlusher>();

export function registerComposerDraftFlusher(flusher: DraftFlusher): () => void {
  draftFlushers.add(flusher);
  return () => draftFlushers.delete(flusher);
}

export async function flushComposerDrafts(): Promise<void> {
  for (const flusher of draftFlushers) await flusher();
}
