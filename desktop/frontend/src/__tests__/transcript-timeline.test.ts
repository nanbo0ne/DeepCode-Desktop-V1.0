import { historyMessagesToItems, initialState, reducer, type Item } from "../lib/useController";
import { activityIndicatorPhase, buildTimelineSegments, requiredWarmPage, timelineKinds, visibleWarmStart } from "../lib/transcriptTimeline";
import { readFileSync } from "node:fs";

let failed = 0;

function equal<T>(label: string, actual: T, expected: T) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    return;
  }
  failed += 1;
  process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
}

const chronology: Item[] = [
  { kind: "user", id: "u1", text: "start" },
  { kind: "assistant", id: "a1", text: "first", reasoning: "", streaming: false },
  { kind: "tool", id: "t1", name: "read", args: "{}", readOnly: true, status: "done" },
  { kind: "assistant", id: "a2", text: "second", reasoning: "", streaming: false },
  { kind: "tool", id: "t2", name: "bash", args: "{}", readOnly: false, status: "done" },
];

equal(
  "a live turn interleaves progress text and tool groups",
  timelineKinds(buildTimelineSegments(chronology, true)),
  ["user", "assistant", "process:tool:read", "assistant", "process:tool:bash"],
);

const adjacent: Item[] = [
  { kind: "user", id: "u1", text: "start" },
  { kind: "assistant", id: "r1", text: "", reasoning: "thinking", streaming: false },
  { kind: "tool", id: "t1", name: "read", args: "{}", readOnly: true, status: "done" },
  { kind: "notice", id: "n1", level: "info", text: "done" },
  { kind: "assistant", id: "a1", text: "answer", reasoning: "", streaming: false },
  { kind: "tool", id: "t2", name: "bash", args: "{}", readOnly: false, status: "done" },
];

equal(
  "reasoning and progress separate consecutive tool groups",
  timelineKinds(buildTimelineSegments(adjacent, true)),
  ["user", "assistant", "process:tool:read,notice", "assistant", "process:tool:bash"],
);

const withStats: Item[] = [
  ...chronology,
  { kind: "assistant", id: "final-1", messageId: "final-1", text: "Done.", reasoning: "", streaming: false, final: true, turnId: "turn-1" },
  { kind: "turn_stats", id: "s1", elapsedMs: 5000, tokens: 120, success: true, outcome: "success", turnId: "turn-1", finalMessageId: "final-1" },
];
equal(
  "successful completed turn collapses intermediate activity",
  timelineKinds(buildTimelineSegments(withStats, false)),
  ["user", "completed:assistant,tool:read,assistant,tool:bash"],
);

const recoveredStats: Item[] = [
  { kind: "user", id: "ru1", text: "fix it" },
  { kind: "tool", id: "rt1", name: "bash", args: "{}", readOnly: false, status: "error", error: "first attempt failed" },
  { kind: "tool", id: "rt2", name: "bash", args: "{}", readOnly: false, status: "done" },
  { kind: "assistant", id: "ra1", messageId: "ra1", text: "Fixed and verified.", reasoning: "", streaming: false, final: true, turnId: "turn-r" },
  { kind: "turn_stats", id: "rs1", elapsedMs: 7000, success: true, outcome: "success", turnId: "turn-r", finalMessageId: "ra1" },
];
equal(
  "explicit successful completion collapses a recovered tool failure",
  timelineKinds(buildTimelineSegments(recoveredStats, false)),
  ["user", "completed:tool:bash,tool:bash"],
);

const failedStats: Item[] = [
  ...chronology,
  { kind: "notice", id: "error", level: "warn", text: "failed" },
  { kind: "turn_stats", id: "s2", elapsedMs: 5000, success: false },
];
equal(
  "failed turn keeps its diagnostic timeline visible",
  timelineKinds(buildTimelineSegments(failedStats, false)),
  ["user", "stats", "assistant", "process:tool:read", "assistant", "process:tool:bash,notice"],
);

equal(
  "final legacy turn without completion evidence remains expanded",
  timelineKinds(buildTimelineSegments(chronology, false)),
  ["user", "assistant", "process:tool:read", "assistant", "process:tool:bash"],
);

const legacyHistory: Item[] = [
  ...chronology,
  { kind: "user", id: "u2", text: "next question" },
  { kind: "assistant", id: "a3", text: "working", reasoning: "", streaming: false },
];
equal(
  "a later user turn does not guess completion for legacy history",
  timelineKinds(buildTimelineSegments(legacyHistory, true)),
  ["user", "assistant", "process:tool:read", "assistant", "process:tool:bash", "user", "assistant"],
);

const failedLegacyHistory: Item[] = [
  { kind: "user", id: "fu1", text: "run" },
  { kind: "tool", id: "ft1", name: "bash", args: "{}", readOnly: false, status: "error", error: "failed" },
  { kind: "assistant", id: "fa1", text: "The command failed.", reasoning: "", streaming: false },
  { kind: "user", id: "fu2", text: "try something else" },
];
equal(
  "legacy failure evidence prevents automatic collapse",
  timelineKinds(buildTimelineSegments(failedLegacyHistory, false)),
  ["user", "process:tool:bash", "assistant", "user"],
);

const completedWithBackgroundNotice: Item[] = [
  ...withStats,
  { kind: "notice", id: "background", level: "info", text: "Background task finished" },
];
equal(
  "unowned background notices stay outside the completed turn",
  timelineKinds(buildTimelineSegments(completedWithBackgroundNotice, false)),
  ["user", "completed:assistant,tool:read,assistant,tool:bash", "process:notice"],
);

const withModeSwitch: Item[] = [
  ...withStats,
  { kind: "mode_switch", id: "switch-1", fromMode: "coding", toMode: "assistant", appliedMode: "assistant", phase: "completed", progress: 100, startedAt: 10, completedAt: 20 },
  { kind: "user", id: "u-next", text: "continue" },
];
equal(
  "mode switches remain standalone and never enter a completed process panel",
  timelineKinds(buildTimelineSegments(withModeSwitch, false)),
  ["user", "completed:assistant,tool:read,assistant,tool:bash", "mode_switch", "user"],
);

const restoredSwitch = historyMessagesToItems([{
  role: "mode_switch",
  content: "",
  switchId: "switch-restored",
  switchFromMode: "assistant",
  switchToMode: "coding",
  switchAppliedMode: "coding",
  switchPhase: "completed",
  switchProgress: 100,
  switchStartedAt: 100,
  switchCompletedAt: 200,
}], "h").items;
equal("persisted mode switch history restores its explicit item type", restoredSwitch[0]?.kind, "mode_switch");

let switching = reducer(initialState, { type: "runtime_switch", progress: {
  tabId: "tab", switchId: "switch-live", generation: 2, fromMode: "coding", toMode: "assistant",
  appliedMode: "coding", phase: "building", progress: 35, recorded: true, startedAt: 100,
} });
switching = reducer(switching, { type: "runtime_switch", progress: {
  tabId: "tab", switchId: "switch-live", generation: 2, fromMode: "coding", toMode: "assistant",
  appliedMode: "assistant", phase: "completed", progress: 100, recorded: true, startedAt: 100, completedAt: 200,
} });
equal("live switch phases update one stable timeline item", switching.items.filter((item) => item.kind === "mode_switch").length, 1);
equal("live switch completion updates the existing item", switching.items.find((item) => item.kind === "mode_switch")?.phase, "completed");
const staleSwitch = reducer(switching, { type: "runtime_switch", progress: {
  tabId: "tab", switchId: "stale", generation: 1, fromMode: "assistant", toMode: "coding",
  phase: "failed", progress: 35, recorded: true, startedAt: 50,
} });
equal("older runtime generations cannot overwrite the latest switch", staleSwitch.items.some((item) => item.kind === "mode_switch" && item.id === "stale"), false);

let protocol = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-p" } });
protocol = reducer(protocol, { type: "event", e: { kind: "text", turnId: "turn-p", itemId: "item-p", messageId: "message-p", text: "Working" } });
protocol = reducer(protocol, { type: "event", e: { kind: "message", turnId: "turn-p", itemId: "item-p", messageId: "message-p", text: "Working" } });
protocol = reducer(protocol, { type: "event", e: { kind: "answer_committed", turnId: "turn-p", finalItemId: "item-p", finalMessageId: "message-p" } });
protocol = reducer(protocol, { type: "event", e: {
  kind: "turn_done", turnId: "turn-p", finalItemId: "item-p", finalMessageId: "message-p", outcome: "success",
  turnTokens: 321, turnCost: 0.0123, turnCurrency: "$", turnCostAvailable: true,
} });
equal("answer_committed owns the exact final message", protocol.items.some((item) => item.kind === "assistant" && item.messageId === "message-p" && item.final), true);
equal("explicit success records a successful turn outcome", protocol.items.some((item) => item.kind === "turn_stats" && item.outcome === "success"), true);
const authoritativeStats = protocol.items.find((item) => item.kind === "turn_stats");
equal("TurnDone tokens use the backend authoritative aggregate", authoritativeStats?.kind === "turn_stats" ? authoritativeStats.tokens : undefined, 321);
equal("official DeepSeek cost is retained only with its source flag", authoritativeStats?.kind === "turn_stats" ? [authoritativeStats.cost, authoritativeStats.currency, authoritativeStats.costAvailable] : undefined, [0.0123, "$", true]);

const restoredStats = historyMessagesToItems([{
  role: "turn_stats", content: "", turnId: "history-turn", outcome: "success", elapsedMs: 1400,
  tokens: 88, cost: 0.0042, currency: "$", costAvailable: true, finalMessageId: "history-final",
}], "h").items[0];
equal(
  "MessageView turn stats restore cost fields",
  restoredStats?.kind === "turn_stats" ? [restoredStats.tokens, restoredStats.cost, restoredStats.currency, restoredStats.costAvailable] : undefined,
  [88, 0.0042, "$", true],
);

const historyAssistant = historyMessagesToItems([{ role: "assistant", content: "history answer" }], "history").items;
const liveAfterHistory = reducer(
  { ...initialState, items: historyAssistant, turnActive: true, currentTurnId: "live-turn", turnStartAt: Date.now() },
  { type: "event", e: { kind: "text", turnId: "live-turn", messageId: "live-message", text: "live answer" } },
);
equal(
  "message-only live events do not match an unidentifiable history assistant",
  liveAfterHistory.items.filter((item) => item.kind === "assistant").length,
  2,
);
equal(
  "message-only live events keep their own assistant identity",
  liveAfterHistory.live?.text,
  "live answer",
);

let cancelled = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-c" } });
cancelled = reducer(cancelled, { type: "event", e: {
  kind: "turn_done", turnId: "turn-c", outcome: "cancelled", err: "context canceled",
  turnTokens: 7, turnCost: 9, turnCurrency: "$", turnCostAvailable: false,
} });
equal("cancelled turns do not render context-canceled as an error", cancelled.items.some((item) => item.kind === "notice" && item.text === "context canceled"), false);
equal("cancelled turns retain their explicit outcome", cancelled.items.some((item) => item.kind === "turn_stats" && item.outcome === "cancelled"), true);
const cancelledStats = cancelled.items.find((item) => item.kind === "turn_stats");
equal("cancelled turns keep authoritative tokens", cancelledStats?.kind === "turn_stats" ? cancelledStats.tokens : undefined, 7);
equal("unavailable turn cost is never rendered as official", cancelledStats?.kind === "turn_stats" ? [cancelledStats.cost, cancelledStats.currency, cancelledStats.costAvailable] : undefined, [undefined, undefined, false]);

let stable = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-stable" } });
stable = reducer(stable, { type: "event", e: { kind: "text", turnId: "turn-stable", itemId: "item-stable", messageId: "message-stable", text: "Done" } });
stable = reducer(stable, { type: "event", e: { kind: "message", turnId: "turn-stable", itemId: "item-stable", messageId: "message-stable", text: "Done" } });
stable = reducer(stable, { type: "event", e: { kind: "answer_committed", turnId: "turn-stable", finalItemId: "item-stable", finalMessageId: "message-stable" } });
stable = reducer(stable, { type: "event", e: { kind: "turn_done", turnId: "turn-stable", finalItemId: "item-stable", finalMessageId: "message-stable", outcome: "success", turnTokens: 4 } });
const stableBefore = buildTimelineSegments(stable.items, false).find((segment) => segment.kind === "completed");
stable = reducer(stable, { type: "user", text: "next", seq: stable.seq });
stable = reducer(stable, { type: "event", e: { kind: "turn_started", turnId: "turn-next" } });
stable = reducer(stable, { type: "event", e: { kind: "turn_done", turnId: "turn-next", outcome: "cancelled" } });
const stableAfter = buildTimelineSegments(stable.items, false).find((segment) => segment.kind === "completed");
equal("a later cancelled turn preserves the previous completed segment identity", stableAfter?.kind === "completed" ? stableAfter.id : undefined, stableBefore?.kind === "completed" ? stableBefore.id : undefined);

let lateDone = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-old" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "text", turnId: "turn-old", itemId: "old-item", messageId: "old-message", text: "old answer" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "message", turnId: "turn-old", itemId: "old-item", messageId: "old-message", text: "old answer" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "answer_committed", turnId: "turn-old", finalItemId: "old-item", finalMessageId: "old-message" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "turn_done", turnId: "turn-old", finalMessageId: "old-message", outcome: "success", turnTokens: 12, turnCost: 0.01, turnCurrency: "$", turnCostAvailable: true } });
lateDone = reducer(lateDone, { type: "user", text: "new answer", seq: lateDone.seq });
lateDone = reducer(lateDone, { type: "event", e: { kind: "turn_started", turnId: "turn-new" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "text", turnId: "turn-new", itemId: "new-item", messageId: "new-message", text: "still working" } });
const afterLateDone = reducer(lateDone, { type: "event", e: {
  kind: "turn_done", turnId: "turn-old", finalMessageId: "old-message", outcome: "success",
  // This aggregate includes a child request that arrived after the first UI completion.
  turnTokens: 48, turnCost: 0.0345, turnCurrency: "$", turnCostAvailable: true,
} });
const refreshedOldStats = afterLateDone.items.find((item) => item.kind === "turn_stats" && item.turnId === "turn-old");
equal("late old TurnDone does not finalize the active new turn", [afterLateDone.running, afterLateDone.turnActive, afterLateDone.currentTurnId, afterLateDone.live?.text], [true, true, "turn-new", "still working"]);
equal("late child-inclusive TurnDone refreshes only the matching old stats", refreshedOldStats?.kind === "turn_stats" ? [refreshedOldStats.tokens, refreshedOldStats.cost, refreshedOldStats.currency, refreshedOldStats.costAvailable] : undefined, [48, 0.0345, "$", true]);
equal("late old TurnDone does not duplicate turn stats", afterLateDone.items.filter((item) => item.kind === "turn_stats").length, 1);

const runningSegments = buildTimelineSegments(chronology, true);
const completedSegments = buildTimelineSegments(failedStats, false);
const lastRunningProcess = [...runningSegments].reverse().find((segment) => segment.kind === "process");
const lastCompletedProcess = [...completedSegments].reverse().find((segment) => segment.kind === "process");
equal(
  "current process segment remains live before TurnDone",
  lastRunningProcess?.kind === "process" ? lastRunningProcess.completed : undefined,
  false,
);
equal(
  "failed process segment stays expanded after TurnDone",
  lastCompletedProcess?.kind === "process" ? lastCompletedProcess.completed : undefined,
  false,
);
equal("model activity rotates clockwise", activityIndicatorPhase(chronology, true, true, false), "model");
const activeTool: Item[] = [
  { kind: "user", id: "atu1", text: "run it" },
  { kind: "tool", id: "att1", name: "bash", args: "{}", readOnly: false, status: "running" },
];
equal("running tool activity rotates counterclockwise", activityIndicatorPhase(activeTool, true, true, false), "tool");
equal("disabled activity mark stays hidden", activityIndicatorPhase(chronology, false, true, false), undefined);
equal("paused activity mark stays hidden", activityIndicatorPhase(chronology, true, true, true), undefined);
equal("completed activity mark stays hidden", activityIndicatorPhase(failedStats, true, false, false), undefined);
equal("latest hidden warm turn needs one page", requiredWarmPage(12, 11, 5), 1);
equal("older warm turn requests enough pages before jumping", requiredWarmPage(12, 2, 5), 2);
equal("cold history starts fully hidden", visibleWarmStart(70, 0, 20), 70);
equal("one page reveals the newest warm turns", visibleWarmStart(70, 1, 20), 50);
equal("enough pages reveal the oldest warm turn", visibleWarmStart(70, 4, 20), 0);

const active = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
const done = reducer(active, { type: "event", e: { kind: "turn_done" } });
const backgroundNotice = reducer(done, {
  type: "event",
  e: { kind: "notice", level: "info", text: "background job finished" },
});
equal("background notice does not reactivate running state", backgroundNotice.running, false);
equal("background notice does not reactivate the turn", backgroundNotice.turnActive, false);

const transcriptSource = readFileSync(new URL("../components/Transcript.tsx", import.meta.url), "utf8");
const transcriptCss = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
equal("process group no longer nests an outer ProcessCard", transcriptSource.includes("<ProcessCard\n      tone=\"default\""), false);
equal("completed timeline has a flat activity rail", transcriptSource.includes('className="completed-turn__timeline process-activity-rail"'), true);
equal("completed process details do not create a nested process panel", transcriptSource.includes("<TimelineProcessGroup\n                      key={segment.id}"), false);
equal("successful completed turns start collapsed", transcriptSource.includes("const [open, setOpen] = useState(false)"), true);
equal("question rail does not scroll the transcript via scrollIntoView", transcriptSource.includes('el?.scrollIntoView({ block: "nearest" })'), false);
equal("warm pagination renders from the hidden-turn boundary", transcriptSource.includes("warmStartTurn = shownWarmStart"), true);
equal("question marker mouse events do not bubble into the rail", transcriptSource.includes("e.stopPropagation();"), true);
equal("activity phase changes wait for the old single ring to fade out", transcriptSource.includes("}, 140);") && transcriptSource.includes("}, 160);"), true);
equal("activity mark renders exactly one phase-keyed spinner element", transcriptSource.includes('<span key={visual.phase} className={`process-activity-spinner process-activity-spinner--${visual.phase}`} />'), true);
equal("activity ring never flips animation direction in place", transcriptCss.includes("animation-direction"), false);
equal("clockwise and counterclockwise rings use separate generated gradients", transcriptCss.includes("process-activity-spin-clockwise") && transcriptCss.includes("process-activity-spin-counterclockwise"), true);
equal("completed turn header is light, bordered, and rounded", transcriptSource.includes("turnStatsHeaderStyle") && transcriptSource.includes("borderRadius: 6"), true);
equal("completed turn cost is guarded by the backend availability flag", transcriptSource.includes("item.costAvailable !== true"), true);
const standaloneStatsSource = transcriptSource.match(/function TurnStatsRow[\s\S]*?\r?\n}\r?\n\r?\nfunction CompletedTurn/);
equal("standalone stats render without a faux expand control", Boolean(standaloneStatsSource && !standaloneStatsSource[0].includes("useState") && !standaloneStatsSource[0].includes("onToggle")), true);

if (failed > 0) process.exit(1);
