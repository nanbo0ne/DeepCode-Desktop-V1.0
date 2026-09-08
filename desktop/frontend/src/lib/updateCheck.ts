import { app } from "./bridge";
import type { UpdateInfo } from "./types";

export const UPDATE_CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;
export const UPDATE_AVAILABLE_EVENT = "orca:update-available";
const UPDATE_CACHE_KEY = "orca.update-check.v1";

interface CachedUpdateCheck {
  checkedAt: number;
  currentVersion: string;
  info: UpdateInfo;
}

interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export function readCachedUpdate(
  currentVersion: string,
  now = Date.now(),
  storage: StorageLike | null = typeof localStorage === "undefined" ? null : localStorage,
): UpdateInfo | null {
  if (!storage) return null;
  try {
    const parsed = JSON.parse(storage.getItem(UPDATE_CACHE_KEY) ?? "null") as CachedUpdateCheck | null;
    if (!parsed || parsed.currentVersion !== currentVersion || !Number.isFinite(parsed.checkedAt)) return null;
    if (now - parsed.checkedAt < 0 || now - parsed.checkedAt >= UPDATE_CHECK_INTERVAL_MS) return null;
    return { ...parsed.info, canDownload: parsed.info.canDownload === true };
  } catch {
    return null;
  }
}

export function writeCachedUpdate(
  currentVersion: string,
  info: UpdateInfo,
  checkedAt = Date.now(),
  storage: StorageLike | null = typeof localStorage === "undefined" ? null : localStorage,
): void {
  if (!storage || info.err) return;
  try {
    storage.setItem(UPDATE_CACHE_KEY, JSON.stringify({ checkedAt, currentVersion, info } satisfies CachedUpdateCheck));
  } catch {
    // Storage failures must never block the application.
  }
}

export function publishUpdate(info: UpdateInfo | null): void {
  if (typeof window === "undefined" || !info?.available) return;
  window.dispatchEvent(new CustomEvent<UpdateInfo>(UPDATE_AVAILABLE_EVENT, { detail: info }));
}

export async function checkDesktopUpdate(currentVersion: string, force = false): Promise<UpdateInfo | null> {
  if (!force) {
    const cached = readCachedUpdate(currentVersion);
    if (cached) {
      publishUpdate(cached);
      return cached;
    }
  }
  const info = await app.CheckUpdate();
  if (!info) return null;
  const normalizedInfo = { ...info, canDownload: info.canDownload === true };
  writeCachedUpdate(currentVersion, normalizedInfo);
  publishUpdate(normalizedInfo);
  return normalizedInfo;
}
