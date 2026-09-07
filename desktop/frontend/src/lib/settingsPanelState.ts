export type LocalDownloadAction = "pause" | "resume" | "cancel" | "retry" | "redownload";

// Keep the UI state machine explicit: a task should never offer resume while it
// is verifying, and terminal states need a path back to a fresh download.
export function localDownloadActions(state: string): LocalDownloadAction[] {
  switch (state) {
    case "downloading":
      return ["pause", "cancel"];
    case "paused":
      return ["resume", "cancel"];
    case "queued":
      return ["cancel"];
    case "verifying":
      return ["cancel"];
    case "failed":
      return ["retry"];
    case "cancelled":
      return ["redownload"];
    default:
      return [];
  }
}

export function wrappedFocusIndex(current: number, length: number, backwards: boolean): number {
  if (length <= 0) return -1;
  if (current < 0 || current >= length) return backwards ? length - 1 : 0;
  return (current + (backwards ? -1 : 1) + length) % length;
}
