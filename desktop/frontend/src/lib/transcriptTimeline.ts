import type { Item } from "./useController";

export type TimelineProcessItem = Exclude<Item, { kind: "user" | "turn_stats" | "mode_switch" }>;

export type TimelineSegment =
  | { kind: "user"; item: Extract<Item, { kind: "user" }> }
  | { kind: "assistant"; item: Extract<Item, { kind: "assistant" }> }
  | { kind: "steer"; item: Extract<Item, { kind: "steer" }> }
  | { kind: "mode_switch"; item: Extract<Item, { kind: "mode_switch" }> }
  | { kind: "process"; id: string; items: TimelineProcessItem[]; completed: boolean }
  | { kind: "stats"; item: Extract<Item, { kind: "turn_stats" }> }
  | {
      kind: "completed";
      id: string;
      stats: Extract<Item, { kind: "turn_stats" }>;
      hidden: Item[];
      final: Extract<Item, { kind: "assistant" }>;
    };

const timelineCache = new WeakMap<readonly Item[], Map<boolean, TimelineSegment[]>>();

export function requiredWarmPage(warmTurnCount: number, questionTurn: number, pageSize: number): number {
  if (warmTurnCount <= 0 || questionTurn >= warmTurnCount || pageSize <= 0) return 0;
  return Math.max(1, Math.ceil((warmTurnCount - Math.max(0, questionTurn)) / pageSize));
}

export function visibleWarmStart(warmTurnCount: number, page: number, pageSize: number): number {
  if (warmTurnCount <= 0) return 0;
  if (page <= 0 || pageSize <= 0) return warmTurnCount;
  return Math.max(0, warmTurnCount - page * pageSize);
}

function visibleProcessItem(item: Item): item is TimelineProcessItem {
  if (item.kind === "assistant") return Boolean(item.reasoning) || item.streaming;
  if (item.kind === "tool") return !item.parentId && item.name !== "todo_write" && item.name !== "exit_plan_mode";
  return item.kind === "notice" || item.kind === "phase" || item.kind === "compaction" || item.kind === "steer";
}

function pushProcess(out: TimelineSegment[], item: TimelineProcessItem, completed: boolean) {
  const last = out[out.length - 1];
  if (last?.kind === "process" && last.completed === completed) {
    last.items.push(item);
    return;
  }
  out.push({ kind: "process", id: `process-${item.id}`, items: [item], completed });
}

function completedTurnSegment(items: readonly Item[], completed: boolean, fallbackID: string): TimelineSegment | null {
  if (!completed) return null;
  const explicitStats = items.find((item): item is Extract<Item, { kind: "turn_stats" }> => item.kind === "turn_stats");
  if (explicitStats && (explicitStats.outcome ?? (explicitStats.success ? "success" : "failed")) !== "success") return null;
  const hasFailureEvidence = items.some((item) =>
    (item.kind === "user" && item.failed) ||
    (item.kind === "tool" && (item.status === "error" || item.status === "stopped")),
  );
  if (!explicitStats && hasFailureEvidence) return null;
  let finalIndex = -1;
  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    if (item.kind === "assistant" && item.final && !item.streaming && item.text.trim() !== "") {
      finalIndex = i;
    }
  }
  // Compatibility for pre-V8 live events. Persisted legacy history is never
  // folded without explicit metadata, so uncertain text remains visible.
  if (finalIndex < 0 && explicitStats && !explicitStats.turnId) {
    for (let i = items.length - 1; i >= 0; i--) {
      const item = items[i];
      if (item.kind === "assistant" && !item.streaming && item.text.trim() !== "") {
        finalIndex = i;
        break;
      }
    }
  }
  if (finalIndex < 0) return null;
  const final = items[finalIndex] as Extract<Item, { kind: "assistant" }>;
  const stats = explicitStats ?? {
    kind: "turn_stats" as const,
    id: `legacy-stats-${fallbackID}`,
    success: true,
    outcome: "success" as const,
  };
  const hidden = items.flatMap((item, index): Item[] => {
    if (item.kind === "user" || item.kind === "turn_stats") return [];
    if (index !== finalIndex) return [item];
    return final.reasoning.trim() !== "" ? [{ ...final, text: "" }] : [];
  });
  if (hidden.length === 0) return null;
  return { kind: "completed", id: `completed-${fallbackID}`, stats, hidden, final: { ...final, reasoning: "" } };
}

function buildTurn(items: readonly Item[], completed: boolean): TimelineSegment[] {
  const out: TimelineSegment[] = [];
  const stats = items.find((item): item is Extract<Item, { kind: "turn_stats" }> => item.kind === "turn_stats");
  const user = items.find((item): item is Extract<Item, { kind: "user" }> => item.kind === "user");
  if (user) out.push({ kind: "user", item: user });
  const collapsed = completedTurnSegment(items, completed, user?.id ?? stats?.id ?? "turn");
  if (collapsed) {
    out.push(collapsed);
    return out;
  }
  if (stats) out.push({ kind: "stats", item: stats });

  items.forEach((item) => {
    if (item.kind === "user" || item.kind === "turn_stats") return;
    if (item.kind === "assistant") {
      // Live text lives outside items until Message arrives. Mount its consumer
      // even when this placeholder has no committed text or reasoning yet.
      if (item.streaming || item.text.trim() || item.reasoning) {
        out.push({ kind: "assistant", item });
      }
      return;
    }
    if (item.kind === "mode_switch") out.push({ kind: "mode_switch", item });
    else if (visibleProcessItem(item)) pushProcess(out, item, completed);
  });
  return out;
}

export function buildTimelineSegments(items: readonly Item[], running: boolean): TimelineSegment[] {
  const cached = timelineCache.get(items)?.get(running);
  if (cached) return cached;

  const groups: Item[][] = [];
  const turnsByID = new Map<string, Item[]>();
  let currentTurn: Item[] | undefined;
  for (const item of items) {
    if (item.kind === "mode_switch") {
      currentTurn = undefined;
      groups.push([item]);
      continue;
    }
    if (item.kind === "user") {
      currentTurn = [item];
      groups.push(currentTurn);
      if (item.turnId) turnsByID.set(item.turnId, currentTurn);
      continue;
    }
    const identifiedTurn = item.turnId ? turnsByID.get(item.turnId) : undefined;
    if (identifiedTurn) {
      identifiedTurn.push(item);
      continue;
    }
    const currentFinished = currentTurn?.some((entry) => entry.kind === "turn_stats") ?? false;
    if (currentTurn && !currentFinished) {
      currentTurn.push(item);
      continue;
    }
    currentTurn = undefined;
    groups.push([item]);
  }

  const out: TimelineSegment[] = [];
  groups.forEach((group) => {
    if (group.some((item) => item.kind === "user")) {
      const explicitlyCompleted = group.some((item) => item.kind === "turn_stats" && (item.outcome ?? (item.success ? "success" : "failed")) === "success");
      out.push(...buildTurn(group, explicitlyCompleted));
      return;
    }
    for (const item of group) {
      if (item.kind === "mode_switch") out.push({ kind: "mode_switch", item });
      else if (item.kind === "steer") out.push({ kind: "steer", item });
      else if (item.kind === "assistant") {
        if (item.streaming || item.reasoning || item.text.trim()) out.push({ kind: "assistant", item });
      } else if (visibleProcessItem(item)) pushProcess(out, item, true);
    }
  });
  const variants = timelineCache.get(items) ?? new Map<boolean, TimelineSegment[]>();
  variants.set(running, out);
  timelineCache.set(items, variants);
  return out;
}

export type ActivityIndicatorPhase = "model" | "tool";

export function activityIndicatorPhase(
  items: readonly Item[],
  enabled: boolean,
  running: boolean,
  paused: boolean,
): ActivityIndicatorPhase | undefined {
  if (!enabled || !running || paused) return undefined;

  let turnStart = 0;
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (items[index].kind === "user") {
      turnStart = index;
      break;
    }
  }
  for (let index = items.length - 1; index >= turnStart; index -= 1) {
    const item = items[index];
    if (item.kind === "tool" && item.status === "running") return "tool";
    if (item.kind === "compaction" && item.pending) return "tool";
  }
  return "model";
}

export function timelineKinds(segments: readonly TimelineSegment[]): string[] {
  return segments.map((segment) => {
    if (segment.kind === "completed") return `completed:${segment.hidden.map((item) => item.kind === "tool" ? `tool:${item.name}` : item.kind).join(",")}`;
    if (segment.kind === "mode_switch") return "mode_switch";
    if (segment.kind !== "process") return segment.kind;
    return `process:${segment.items.map((item) => item.kind === "tool" ? `tool:${item.name}` : item.kind).join(",")}`;
  });
}
