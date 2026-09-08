import type { UpdateProgressPhase } from "./types";

export interface UpdateDismissalState {
  version: string;
  dismissed: boolean;
}

export function acceptUpdate(state: UpdateDismissalState, version: string): UpdateDismissalState {
  return state.version === version ? state : { version, dismissed: false };
}

export function dismissUpdate(state: UpdateDismissalState, version: string): UpdateDismissalState {
  return state.version === version ? { ...state, dismissed: true } : { version, dismissed: true };
}

export function reopenUpdate(state: UpdateDismissalState, version: string): UpdateDismissalState {
  return state.version === version ? { ...state, dismissed: false } : { version, dismissed: false };
}

export function shouldShowUpdate(state: UpdateDismissalState, version: string): boolean {
  return state.version !== version || !state.dismissed;
}

export function canCancelUpdateDownload(phase: UpdateProgressPhase): boolean {
  return phase === "downloading" || phase === "verifying";
}

export async function runReadyUpdateAction({
  selfUpdate,
  flush,
  apply,
  openDownloadFolder,
}: {
  selfUpdate: boolean;
  flush: () => Promise<void>;
  apply: () => Promise<void>;
  openDownloadFolder: () => Promise<void>;
}): Promise<void> {
  if (selfUpdate) {
    await flush();
    await apply();
  } else {
    await openDownloadFolder();
  }
}
