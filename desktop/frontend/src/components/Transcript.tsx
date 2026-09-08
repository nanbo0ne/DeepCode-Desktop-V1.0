import { createContext, memo, type CSSProperties, type MouseEvent as ReactMouseEvent, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { Item, LiveStream } from "../lib/useController";
import type { CheckpointMeta, JobView, ProcessDisplayMode } from "../lib/types";
import { activityIndicatorPhase, buildTimelineSegments, requiredWarmPage, visibleWarmStart, type ActivityIndicatorPhase, type TimelineProcessItem } from "../lib/transcriptTimeline";
import { useLayoutEffect } from "react";
import { useT } from "../lib/i18n";
import { replaceAttachmentRefsForDisplay } from "../lib/attachmentDisplay";
import { AssistantMessage, TurnActions, UserMessage } from "./Message";
import { ProcessBrainIcon, ProcessCard, ProcessCompactIcon, ProcessInfoIcon, ProcessPhaseIcon, ProcessStatusIcon, ProcessToolIcon } from "./ProcessCard";
import { ToolCard } from "./ToolCard";
import { ArrowDown, ArrowRightLeft, Check, ChevronRight, TriangleAlert } from "lucide-react";
import { Welcome } from "./Welcome";

type ToolItem = Extract<Item, { kind: "tool" }>;
type AssistantItem = Extract<Item, { kind: "assistant" }>;
type TurnStatsItem = Extract<Item, { kind: "turn_stats" }>;
type OpenTurnAction = { turn: number; menu: "summary" | "rewind" };
type QuestionAnchor = { id: string; text: string; turn: number };

const QUESTION_NAV_MIN_COUNT = 2;
const LiveStreamContext = createContext<LiveStream | undefined>(undefined);

const LiveReasoningMessage = memo(function LiveReasoningMessage({ item }: { item: AssistantItem }) {
  const live = useContext(LiveStreamContext);
  const shown = live && live.id === item.id
    ? { ...item, text: "", reasoning: live.reasoning, streaming: true }
    : { ...item, text: "" };
  return <AssistantMessage item={shown} defaultExpanded />;
});

function renderProcessItem(
  item: Item,
  subcalls: ReadonlyMap<string, ToolItem[]>,
  liveToolID: string,
  defaultExpandThinking: boolean,
): ReactNode {
  switch (item.kind) {
    case "assistant":
      return [
        defaultExpandThinking && (item.streaming || item.reasoning) && (item.streaming
          ? <LiveReasoningMessage key={`${item.id}-reasoning`} item={item} />
          : <AssistantMessage key={`${item.id}-reasoning`} item={{ ...item, text: "" }} defaultExpanded={defaultExpandThinking} />),
        item.text.trim() !== "" && <div className="process-progress-row" key={`${item.id}-progress`}>{item.text}</div>,
      ];
    case "tool":
      if (item.parentId || item.name === "todo_write" || item.name === "exit_plan_mode") return null;
      return <ToolCard key={item.id} item={item} subcalls={subcalls.get(item.id)} livePulse={item.id === liveToolID} />;
    case "phase":
      return <PhaseCard key={item.id} text={item.text} />;
    case "notice":
      return <NoticeCard key={item.id} level={item.level} text={item.text} />;
    case "compaction":
      return <CompactionCard key={item.id} item={item} />;
    case "steer":
      return <SteerCard key={item.id} text={item.text} />;
    default:
      return null;
  }
}

function BackgroundJobsCard({ jobs }: { jobs: JobView[] }) {
  const t = useT();
  const label = jobs.length === 1 ? jobs[0].label || jobs[0].id : t("process.timeline.backgroundCount", { n: jobs.length });
  return (
    <ProcessCard
      tone="accent"
      icon={<ProcessPhaseIcon size={12} />}
      kind={t("process.timeline.background")}
      name={t("process.timeline.backgroundRunning", { label })}
      meta={<ProcessStatusIcon state="running" label={t("process.timeline.backgroundRunning", { label })} />}
      className="background-jobs-card"
    />
  );
}

// Layer budgets
// Hot zone: the most recent N user turns are always fully rendered. All data
// stays in memory (items[]), so expanding a warm turn is instant - no API call.
// Cold zone: a "load more" button paginates the warm zone in batches.
//
//   items[0]
//   ...        Cold zone - paginated, shown on "load more"
//              warmTurnStart
//   ...        Warm zone - collapsible summary cards (individual expand)
//              hotStartIdx
//   items[N]   Hot zone - fully rendered
//   ...
//   items[end]

const HOT_TURNS = 30;
const WARM_PAGE_SIZE = 20; // cold-zone pagination batch
// Helpers
// Helpers

function questionAnchorId(id: string): string {
  return `question-anchor-${id}`;
}

function compactQuestionText(text: string): string {
  const cleaned = replaceAttachmentRefsForDisplay(text).replace(/\s+/g, " ").trim();
  if (cleaned.length <= 80) return cleaned;
  return cleaned.slice(0, 80);
}

function scrollVersion(items: Item[]): string {
  const last = items[items.length - 1];
  if (!last) return "empty";
  switch (last.kind) {
    case "assistant":
      return `${items.length}:${last.id}:a:${last.text?.length ?? 0}:${last.reasoning?.length ?? 0}:${last.streaming ? 1 : 0}`;
    case "tool":
      return `${items.length}:${last.id}:t:${last.name}:${last.status}:${last.args?.length ?? 0}:${last.output?.length ?? 0}:${last.error?.length ?? 0}:${last.truncated ? 1 : 0}`;
    case "turn_stats":
      return `${items.length}:${last.id}:s:${last.elapsedMs}:${last.tokens ?? 0}`;
    case "compaction":
      return `${items.length}:${last.id}:c:${last.pending ? 1 : 0}:${last.summary.length}`;
    case "mode_switch":
      return `${items.length}:${last.id}:m:${last.phase}:${last.progress}`;
    default:
      return `${items.length}:${last.id}:${last.kind}`;
  }
}

function ModeSwitchRow({ item }: { item: Extract<Item, { kind: "mode_switch" }> }) {
  const t = useT();
  const terminal = item.phase === "completed" || item.phase === "failed" || item.phase === "interrupted";
  const failed = item.phase === "failed" || item.phase === "interrupted";
  const label = item.phase === "completed"
    ? t("runtimeSwitch.completed")
    : item.phase === "failed"
      ? t("runtimeSwitch.failed")
      : item.phase === "interrupted"
        ? t("runtimeSwitch.interrupted")
        : t("runtimeSwitch.switching");
  const from = t(`composer.promptMode.${item.fromMode}`);
  const to = t(`composer.promptMode.${item.toMode}`);
  return (
    <div className={`mode-switch-row mode-switch-row--${item.phase}`} role={failed ? "alert" : "status"} title={item.error || undefined}>
      <span className="mode-switch-row__icon" aria-hidden="true">
        {failed ? <TriangleAlert size={14} /> : terminal ? <Check size={14} /> : <ArrowRightLeft size={14} />}
      </span>
      <span className="mode-switch-row__label">{label}</span>
      <span className="mode-switch-row__route">{from} → {to}</span>
    </div>
  );
}

function TimelineProcessGroup({
  items,
  subcalls,
  liveToolID,
  mode,
  completed,
  open,
  onOpenChange,
  activity,
}: {
  items: TimelineProcessItem[];
  subcalls: ReadonlyMap<string, ToolItem[]>;
  liveToolID: string;
  mode: ProcessDisplayMode;
  completed: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  activity?: ProcessActivityVisual;
}) {
  const t = useT();
  const live = useContext(LiveStreamContext);
  const visible = items.filter(isProcessItem);
  if (visible.length === 0) return null;

  let label = t("process.compact.thinking");
  const runningTool = [...visible].reverse().find((item): item is ToolItem => item.kind === "tool" && item.status === "running");
  const toolCount = visible.filter((item) => item.kind === "tool").length;
  if (!completed && runningTool) label = runningTool.readOnly ? t("process.compact.reading") : t("process.compact.tool");
  else if (!completed && visible.some((item) => item.kind === "compaction" && item.pending)) label = t("process.compact.compacting");
  else if (!completed && live?.text?.trim()) label = t("process.compact.answering");
  else if (toolCount === 1) label = t("process.timeline.oneTool");
  else if (toolCount > 1) label = t("process.timeline.tools", { n: toolCount });
  else if (visible.some((item) => item.kind === "compaction")) label = t("process.timeline.compaction");
  const running = visible.some(isProcessItemRunning);

  const toggle = () => onOpenChange(!open);
  return (
    <div
      className={`timeline-process-group${mode === "compact" ? " timeline-process-group--compact" : ""}`}
      data-open={open}
      data-ui-surface="panel"
    >
      <button
        type="button"
        className="timeline-process-group__head"
        onClick={toggle}
        onKeyDown={(event) => {
          if (event.key === "Escape" && open) {
            event.preventDefault();
            onOpenChange(false);
          }
        }}
        aria-expanded={open}
      >
        <span className="timeline-process-group__icon">
          {toolCount > 0 ? <ProcessToolIcon size={13} /> : <ProcessBrainIcon size={13} />}
        </span>
        <span className="timeline-process-group__label">{label}</span>
        {running && <ProcessStatusIcon state="running" label={label} />}
        <ChevronRight className="timeline-process-group__chevron" size={13} aria-hidden="true" />
      </button>
      {open && (
        <div className="timeline-process-group__details process-activity-rail">
          {visible.map((item) => renderProcessItem(item, subcalls, liveToolID, mode === "detailed"))}
        </div>
      )}
      {activity?.phase && <ProcessActivityMark visual={activity} />}
    </div>
  );
}

type ProcessActivityStage = "entering" | "steady" | "exiting" | "idle";
type ProcessActivityVisual = { phase?: ActivityIndicatorPhase; stage: ProcessActivityStage };

function useActivityPhaseTransition(targetPhase?: ActivityIndicatorPhase): ProcessActivityVisual {
  const reducedMotion = useRef(typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches).current;
  const [visual, setVisual] = useState<ProcessActivityVisual>(() => ({
    phase: targetPhase,
    stage: targetPhase ? (reducedMotion ? "steady" : "entering") : "idle",
  }));
  const targetRef = useRef(targetPhase);
  const phaseRef = useRef(targetPhase);
  const stageRef = useRef<ProcessActivityStage>(visual.stage);
  const timerRef = useRef<number | null>(null);

  const commit = useCallback((next: ProcessActivityVisual) => {
    phaseRef.current = next.phase;
    stageRef.current = next.stage;
    setVisual(next);
  }, []);

  const clearTimer = useCallback(() => {
    if (timerRef.current === null) return;
    window.clearTimeout(timerRef.current);
    timerRef.current = null;
  }, []);

  const enter = useCallback((phase: ActivityIndicatorPhase) => {
    clearTimer();
    commit({ phase, stage: reducedMotion ? "steady" : "entering" });
    if (reducedMotion) return;
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      if (phaseRef.current === phase && stageRef.current === "entering") commit({ phase, stage: "steady" });
    }, 160);
  }, [clearTimer, commit, reducedMotion]);

  useEffect(() => {
    targetRef.current = targetPhase;
    if (reducedMotion) {
      clearTimer();
      commit({ phase: targetPhase, stage: targetPhase ? "steady" : "idle" });
      return;
    }
    if (stageRef.current === "exiting") return;
    const currentPhase = phaseRef.current;
    if (!currentPhase) {
      if (targetPhase) enter(targetPhase);
      return;
    }
    if (currentPhase === targetPhase) {
      if (stageRef.current === "entering" && timerRef.current === null) enter(currentPhase);
      return;
    }
    clearTimer();
    commit({ phase: currentPhase, stage: "exiting" });
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      const nextPhase = targetRef.current;
      if (nextPhase) enter(nextPhase);
      else commit({ stage: "idle" });
    }, 140);
  }, [clearTimer, commit, enter, reducedMotion, targetPhase]);

  useEffect(() => () => clearTimer(), [clearTimer]);
  return visual;
}

function ProcessActivityMark({ visual }: { visual: ProcessActivityVisual }) {
  if (!visual.phase) return null;
  return (
    <div className={`process-activity-mark process-activity-mark--${visual.stage}`} aria-hidden="true">
      <span key={visual.phase} className={`process-activity-spinner process-activity-spinner--${visual.phase}`} />
    </div>
  );
}

const LiveAssistantText = memo(function LiveAssistantText({ item, showReasoning }: { item: AssistantItem; showReasoning: boolean }) {
  const live = useContext(LiveStreamContext);
  const current = live && live.id === item.id ? { ...item, text: live.text, reasoning: live.reasoning, streaming: true } : item;
  const shown = { ...current, reasoning: showReasoning ? current.reasoning : "" };
  if (!shown.text && !shown.reasoning) return null;
  return <AssistantMessage item={shown} defaultExpanded />;
});

function repinIfWasPinned(
  el: HTMLDivElement,
  stick: { current: boolean },
  frame: { current: number | null },
) {
  if (!stick.current) return;
  if (frame.current !== null) cancelAnimationFrame(frame.current);
  frame.current = requestAnimationFrame(() => {
    if (stick.current) el.scrollTop = el.scrollHeight;
    frame.current = null;
  });
}

function isAtBottom(el: HTMLDivElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= 4;
}

function scrollElementToBottom(el: HTMLDivElement) {
  el.scrollTop = el.scrollHeight;
}

function lastRunningToolID(items: Item[]): string {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item.kind === "tool" && item.status === "running") return item.id;
  }
  return "";
}

function isProcessItem(item: Item): boolean {
  if (item.kind === "assistant") return Boolean(item.reasoning) || item.text.trim() !== "" || item.streaming;
  if (item.kind === "tool") return !item.parentId && item.name !== "todo_write" && item.name !== "exit_plan_mode";
  return item.kind === "notice" || item.kind === "phase" || item.kind === "compaction" || item.kind === "steer";
}

function isProcessItemRunning(item: Item): boolean {
  if (item.kind === "assistant") return item.streaming;
  if (item.kind === "tool") return item.status === "running";
  if (item.kind === "compaction") return item.pending;
  return false;
}

function formatTurnElapsed(ms: number): string {
  const totalSeconds = Math.max(0, Math.round(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  if (hours > 0) return `${hours}hr${pad(minutes)}min${pad(seconds)}s`;
  if (minutes > 0) return `${minutes}min${pad(seconds)}s`;
  return `${seconds}s`;
}

function turnStatsLabel(t: ReturnType<typeof useT>, item: TurnStatsItem): string {
  const outcome = item.outcome ?? (item.success ? "success" : "failed");
  if (outcome !== "success") {
    const key = outcome === "cancelled"
      ? "process.timeline.cancelled"
      : outcome === "interrupted"
        ? "process.timeline.interrupted"
        : "process.timeline.failed";
    if (typeof item.elapsedMs !== "number") return t(key);
    return t(`${key}Elapsed`, { elapsed: formatTurnElapsed(item.elapsedMs) });
  }
  if (typeof item.elapsedMs !== "number") return t("process.timeline.complete");
  return t("process.timeline.elapsed", { elapsed: formatTurnElapsed(item.elapsedMs) });
}

function turnStatsTokenLabel(t: ReturnType<typeof useT>, item: TurnStatsItem): string {
  return typeof item.tokens === "number" && item.tokens > 0
    ? t("process.timeline.tokens", { n: item.tokens.toLocaleString() })
    : t("process.timeline.tokensPending");
}

function turnStatsCostLabel(t: ReturnType<typeof useT>, item: TurnStatsItem): string | undefined {
  if (item.costAvailable !== true || typeof item.cost !== "number" || !item.currency?.trim()) return undefined;
  const amount = item.cost < 1 ? item.cost.toFixed(4) : item.cost.toFixed(2);
  return t("process.timeline.deepSeekCost", { amount: `${item.currency}${amount}` });
}

const turnStatsHeaderStyle: CSSProperties = {
  width: "100%",
  minHeight: 32,
  boxSizing: "border-box",
  display: "flex",
  alignItems: "center",
  gap: 4,
  justifyContent: "flex-start",
  flexWrap: "wrap",
  padding: "6px 8px",
  border: "1px solid var(--border-soft)",
  borderRadius: 6,
  background: "var(--surface-subtle, #fafbfb)",
};

function TurnStatsHeader({
  item,
  open,
  onToggle,
}: {
  item: TurnStatsItem;
  open?: boolean;
  onToggle?: () => void;
}) {
  const t = useT();
  const costLabel = turnStatsCostLabel(t, item);
  const content = (
    <>
      <span>{turnStatsLabel(t, item)}</span>
      <span>{turnStatsTokenLabel(t, item)}</span>
      {costLabel && <span>{costLabel}</span>}
    </>
  );
  if (!onToggle) return <div style={turnStatsHeaderStyle}>{content}</div>;
  return (
    <button type="button" onClick={onToggle} aria-expanded={Boolean(open)} style={turnStatsHeaderStyle}>
      {content}
      <ChevronRight size={12} aria-hidden="true" />
    </button>
  );
}

function TurnStatsRow({ item }: { item: TurnStatsItem }) {
  return (
    <div className="turn-stats-row">
      <TurnStatsHeader item={item} />
    </div>
  );
}

function CompletedTurn({
  stats,
  hidden,
  final,
  subcalls,
  liveToolID,
  showReasoning,
}: {
  stats: TurnStatsItem;
  hidden: Item[];
  final: AssistantItem;
  subcalls: ReadonlyMap<string, ToolItem[]>;
  liveToolID: string;
  showReasoning: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <div className={`turn-stats-row turn-process-panel${open ? " turn-stats-row--open" : ""}`}>
        <TurnStatsHeader item={stats} open={open} onToggle={() => setOpen((value) => !value)} />
        {open && (
          <div className="completed-turn__details">
            <div className="completed-turn__timeline process-activity-rail">
              {hidden.map((item) => renderProcessItem(item, subcalls, liveToolID, showReasoning))}
            </div>
          </div>
        )}
      </div>
      <AssistantMessage item={final} />
    </>
  );
}

// Summarise a warm turn for its compact card.
function warmUserPreview(text: string): string {
  const cleaned = replaceAttachmentRefsForDisplay(text).replace(/\s+/g, " ").trim();
  return cleaned.length <= 80 ? cleaned : cleaned.slice(0, 77) + "...";
}

// Turn grouping
// A turn is everything from one UserMessage up to (but not including) the next
// UserMessage. This grouping is used only for warm-zone rendering; the hot zone
// still uses the flat items array to preserve the existing rendering logic.

interface TurnGroup {
  userItem: Item;
  assistantPreview: string;
  toolCount: number;
  startIdx: number; // first index in items[] (the user message)
  endIdx: number;   // exclusive end
}

interface TranscriptStructure {
  questions: QuestionAnchor[];
  subcallsByParent: Map<string, ToolItem[]>;
  turnGroups: TurnGroup[];
  hotStartIdx: number;
  contentVersion: string;
  liveToolID: string;
}

const transcriptStructureCache = new WeakMap<readonly Item[], TranscriptStructure>();

function buildTurnGroups(items: Item[], questions: QuestionAnchor[]): TurnGroup[] {
  const groups: TurnGroup[] = [];
  let turnIdx = 0;
  let start = -1;
  for (let i = 0; i < items.length; i++) {
    if (items[i].kind === "user") {
      if (start >= 0) {
        // finalise previous turn
        groups[groups.length - 1].endIdx = i;
      }
      start = i;
      turnIdx = questions.findIndex((q) => q.id === items[i].id);
      if (turnIdx < 0) turnIdx = groups.length;
      groups.push({
        userItem: items[i],
        assistantPreview: "",
        toolCount: 0,
        startIdx: i,
        endIdx: items.length,
      });
    } else if (start >= 0 && groups.length > 0) {
      const g = groups[groups.length - 1];
      const it = items[i];
      if (it.kind === "assistant" && !it.streaming) {
        const previewText = it.text?.trim() || "";
        if (previewText) {
          g.assistantPreview = warmUserPreview(previewText);
        }
      }
      if (it.kind === "tool" && !it.parentId) {
        g.toolCount++;
      }
    }
  }
  return groups;
}

function deriveTranscriptStructure(items: Item[]): TranscriptStructure {
  const cached = transcriptStructureCache.get(items);
  if (cached) return cached;

  const questions: QuestionAnchor[] = [];
  const subcallsByParent = new Map<string, ToolItem[]>();
  let hotStartIdx = 0;
  let remainingHotTurns = HOT_TURNS;
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (items[index].kind !== "user") continue;
    remainingHotTurns -= 1;
    if (remainingHotTurns <= 0) {
      hotStartIdx = index;
      break;
    }
  }
  for (const item of items) {
    if (item.kind === "user") {
      questions.push({ id: item.id, text: compactQuestionText(item.text), turn: questions.length });
    }
    if (item.kind === "tool" && item.parentId) {
      const calls = subcallsByParent.get(item.parentId) ?? [];
      calls.push(item);
      subcallsByParent.set(item.parentId, calls);
    }
  }

  const structure: TranscriptStructure = {
    questions,
    subcallsByParent,
    turnGroups: buildTurnGroups(items, questions),
    hotStartIdx,
    contentVersion: scrollVersion(items),
    liveToolID: lastRunningToolID(items),
  };
  transcriptStructureCache.set(items, structure);
  return structure;
}

function TimelineItems({
  items,
  running,
  processDisplayMode,
  subcalls,
  liveToolID,
  userTurnMap,
  checkpoints,
  openAction,
  actionPending,
  rewindDisabled,
  onRewind,
  onEditUserMessage,
  setOpenAction,
  activityIndicatorEnabled = true,
  paused = false,
}: {
  items: readonly Item[];
  running: boolean;
  processDisplayMode: ProcessDisplayMode;
  subcalls: ReadonlyMap<string, ToolItem[]>;
  liveToolID: string;
  userTurnMap: ReadonlyMap<string, number>;
  checkpoints: ReadonlyMap<number, CheckpointMeta>;
  openAction: OpenTurnAction | null;
  actionPending: boolean;
  rewindDisabled: boolean;
  onRewind: ((turn: number, scope: string) => void) | undefined;
  onEditUserMessage?: (text: string) => void;
  setOpenAction: (action: OpenTurnAction | null) => void;
  activityIndicatorEnabled?: boolean;
  paused?: boolean;
}) {
  const segments = useMemo(() => buildTimelineSegments(items, running), [items, running]);
  const [processOpenOverrides, setProcessOpenOverrides] = useState<Map<string, boolean>>(() => new Map());
  const activityPhase = activityIndicatorPhase(items, activityIndicatorEnabled, running, paused);
  const activity = useActivityPhaseTransition(activityPhase);
  const currentTurnStart = segments.reduce((last, segment, index) => segment.kind === "user" ? index : last, -1);
  const activeProcessSegment = activity.phase
    ? [...segments.slice(currentTurnStart + 1)].reverse().find((segment) => segment.kind === "process")
    : undefined;
  const nodes: ReactNode[] = [];
  let activeTurn: number | undefined;
  let actionText = "";

  const pushTurnActions = (completed: boolean) => {
    if (!completed || activeTurn == null || actionText.trim() === "") return;
    const turn = activeTurn;
    const openMenu = openAction && openAction.turn === turn ? openAction.menu : null;
    nodes.push(
      <div className="timeline-entry timeline-entry--actions" data-transcript-anchor={`actions-${turn}`} key={`ta-${turn}`}>
        <TurnActions
          text={actionText}
          turn={turn}
          openMenu={openMenu}
          onOpenMenu={(menu) => setOpenAction(menu ? { turn, menu } : null)}
          checkpoint={checkpoints.get(turn)}
          actionPending={actionPending}
          rewindDisabled={rewindDisabled}
          onRewind={(targetTurn, scope) => {
            onRewind?.(targetTurn, scope);
            setOpenAction(null);
          }}
        />
      </div>,
    );
    actionText = "";
  };

  for (const segment of segments) {
    switch (segment.kind) {
      case "user": {
        pushTurnActions(true);
        const turn = userTurnMap.get(segment.item.id);
        activeTurn = turn;
        actionText = "";
        nodes.push(
          <div className="timeline-entry timeline-entry--user" data-transcript-anchor={segment.item.id} key={segment.item.id}>
            <UserMessage
              text={segment.item.text}
              failed={segment.item.failed}
              turn={turn}
              anchorId={questionAnchorId(segment.item.id)}
              onEdit={onEditUserMessage}
            />
          </div>,
        );
        break;
      }
      case "stats":
        nodes.push(
          <div className="timeline-entry timeline-entry--stats" data-transcript-anchor={segment.item.id} key={segment.item.id}>
            <TurnStatsRow item={segment.item} />
          </div>,
        );
        break;
      case "completed":
        actionText = segment.final.text;
        nodes.push(
          <div className="timeline-entry timeline-entry--completed" data-transcript-anchor={segment.id} key={segment.id}>
            <CompletedTurn
              stats={segment.stats}
              hidden={segment.hidden}
              final={segment.final}
              subcalls={subcalls}
              liveToolID={liveToolID}
              showReasoning={processDisplayMode === "detailed"}
            />
          </div>,
        );
        break;
      case "assistant":
        if (!segment.item.streaming && segment.item.text.trim() !== "") actionText = segment.item.text;
        nodes.push(
          <div className="timeline-entry timeline-entry--assistant" data-transcript-anchor={`${segment.item.id}-text`} key={`${segment.item.id}-text`}>
            <LiveAssistantText item={segment.item} showReasoning={processDisplayMode === "detailed"} />
          </div>,
        );
        break;
      case "process":
        const segmentOpen = processOpenOverrides.get(segment.id) ?? true;
        nodes.push(
          <div className="timeline-entry timeline-entry--process" data-transcript-anchor={segment.id} key={segment.id}>
            <TimelineProcessGroup
              items={segment.items}
              subcalls={subcalls}
              liveToolID={liveToolID}
              mode={processDisplayMode}
              completed={segment.completed}
              open={segmentOpen}
              onOpenChange={(nextOpen) => setProcessOpenOverrides((current) => {
                const next = new Map(current);
                next.set(segment.id, nextOpen);
                return next;
              })}
              activity={activeProcessSegment?.kind === "process" && activeProcessSegment.id === segment.id ? activity : undefined}
            />
          </div>,
        );
        break;
      case "steer":
        nodes.push(
          <div className="timeline-entry timeline-entry--steer" data-transcript-anchor={segment.item.id} key={segment.item.id}>
            <SteerCard text={segment.item.text} />
          </div>,
        );
        break;
      case "mode_switch":
        pushTurnActions(true);
        nodes.push(
          <div className="timeline-entry timeline-entry--mode-switch" data-transcript-anchor={segment.item.id} key={segment.item.id}>
            <ModeSwitchRow item={segment.item} />
          </div>,
        );
        break;
    }
  }
  if (activity.phase && activeProcessSegment?.kind !== "process") {
    nodes.push(
      <div className="timeline-entry timeline-entry--activity" data-transcript-anchor="current-turn-activity" key="current-turn-activity">
        <div className="timeline-process-group timeline-process-group--activity-only" data-ui-surface="panel">
          <ProcessActivityMark visual={activity} />
        </div>
      </div>,
    );
  }
  pushTurnActions(!running);
  return nodes;
}

// Transcript component

export function Transcript({
  items,
  live,
  running = false,
  footerHeight = 0,
  onPrompt,
  onEditUserMessage,
  onRewind,
  checkpoints = [],
  actionPending = false,
  rewindDisabled = false,
  questionNavigator = true,
  processDisplayMode = "compact",
  activityIndicatorEnabled = true,
  paused = false,
  hydrating = false,
  followButton = true,
  jobs = [],
}: {
  items: Item[];
  live?: LiveStream;
  running?: boolean;
  footerHeight?: number;
  onPrompt: (text: string) => void;
  onEditUserMessage?: (text: string) => void;
  onRewind?: (turn: number, scope: string) => void;
  checkpoints?: CheckpointMeta[];
  actionPending?: boolean;
  rewindDisabled?: boolean;
  questionNavigator?: boolean;
  processDisplayMode?: ProcessDisplayMode;
  activityIndicatorEnabled?: boolean;
  paused?: boolean;
  hydrating?: boolean;
  followButton?: boolean;
  jobs?: JobView[];
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const resizeFrame = useRef<number | null>(null);
  const viewportAnchor = useRef<{ id: string; top: number } | null>(null);
  const pendingQuestionJump = useRef<QuestionAnchor | null>(null);
  const [showFollowButton, setShowFollowButton] = useState(false);
  const [activeJumpTurn, setActiveJumpTurn] = useState<number | null>(null);
  const t = useT();
  const captureViewportAnchor = useCallback((el: HTMLDivElement | null) => {
    const content = contentRef.current;
    if (!el || !content || stick.current) return;
    const scrollerTop = el.getBoundingClientRect().top;
    const nodes = content.querySelectorAll<HTMLElement>("[data-transcript-anchor]");
    for (const node of nodes) {
      const rect = node.getBoundingClientRect();
      if (rect.bottom < scrollerTop + 1) continue;
      const id = node.dataset.transcriptAnchor;
      if (id) viewportAnchor.current = { id, top: rect.top };
      return;
    }
  }, []);

  const restoreViewportAnchor = useCallback((el: HTMLDivElement | null) => {
    if (!el || stick.current) return;
    const saved = viewportAnchor.current;
    if (!saved) {
      captureViewportAnchor(el);
      return;
    }
    const node = contentRef.current?.querySelector<HTMLElement>(`[data-transcript-anchor="${CSS.escape(saved.id)}"]`);
    if (node) {
      const delta = node.getBoundingClientRect().top - saved.top;
      if (Math.abs(delta) >= 0.5) el.scrollTop += delta;
    }
    captureViewportAnchor(el);
  }, [captureViewportAnchor]);

  const structure = useMemo(() => deriveTranscriptStructure(items), [items]);
  const { questions, subcallsByParent, turnGroups, hotStartIdx, contentVersion, liveToolID } = structure;
  const showQuestionNav = questionNavigator && questions.length >= QUESTION_NAV_MIN_COUNT;

  const updateActiveJumpTurn = useCallback((el: HTMLDivElement | null) => {
    if (!el || questions.length === 0) {
      setActiveJumpTurn(null);
      return;
    }
    const scrollerRect = el.getBoundingClientRect();
    const targetY = scrollerRect.top + Math.min(scrollerRect.height * 0.38, 220);
    let active = questions[0]?.turn ?? null;
    for (const question of questions) {
      const node = document.getElementById(questionAnchorId(question.id));
      if (!node) continue;
      const rect = node.getBoundingClientRect();
      if (rect.top <= targetY) active = question.turn;
      else break;
    }
    setActiveJumpTurn((current) => (current === active ? current : active));
  }, [questions]);

  const updateFollowButton = useCallback((el: HTMLDivElement | null) => {
    if (!el) return;
    setShowFollowButton(followButton && !isAtBottom(el));
  }, [followButton]);

  const scrollToBottom = useCallback((follow: boolean) => {
    const el = scrollRef.current;
    if (!el) return;
    stick.current = follow;
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    requestAnimationFrame(() => {
      scrollElementToBottom(el);
      viewportAnchor.current = null;
      setShowFollowButton(false);
    });
  }, []);

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    if (!stick.current) captureViewportAnchor(el);
    updateFollowButton(el);
    updateActiveJumpTurn(el);
  };

  const leaveFollowMode = () => {
    const el = scrollRef.current;
    if (!el || !stick.current) return;
    stick.current = false;
    captureViewportAnchor(el);
    updateFollowButton(el);
  };

  // Track question count so we can detect when the user sends a new message.
  const prevQuestionsLen = useRef(0);

  // A newly submitted message follows the live turn. Explicit user scrolling
  // disables follow mode until the jump-to-latest button is pressed.
  useEffect(() => {
    if (questions.length > prevQuestionsLen.current) {
      const el = scrollRef.current;
      if (el) {
        requestAnimationFrame(() => {
          stick.current = true;
          scrollElementToBottom(el);
          viewportAnchor.current = null;
          updateFollowButton(el);
          updateActiveJumpTurn(el);
        });
      }
    }
    prevQuestionsLen.current = questions.length;
  }, [questions, updateActiveJumpTurn, updateFollowButton]);

  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (!stick.current) {
      restoreViewportAnchor(el);
      updateFollowButton(el);
      updateActiveJumpTurn(el);
      return;
    }
    scrollElementToBottom(el);
    setShowFollowButton(false);
    updateActiveJumpTurn(el);
  }, [contentVersion, live?.text?.length ?? 0, live?.reasoning?.length ?? 0, restoreViewportAnchor, updateActiveJumpTurn, updateFollowButton]);

  useEffect(() => {
    const el = scrollRef.current;
    const content = contentRef.current;
    if (!el || !content || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      if (stick.current) repinIfWasPinned(el, stick, resizeFrame);
      else restoreViewportAnchor(el);
      updateFollowButton(el);
      updateActiveJumpTurn(el);
    });
    observer.observe(content);
    return () => {
      observer.disconnect();
      if (resizeFrame.current !== null) {
        cancelAnimationFrame(resizeFrame.current);
        resizeFrame.current = null;
      }
    };
  }, [restoreViewportAnchor, updateActiveJumpTurn, updateFollowButton]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    repinIfWasPinned(el, stick, resizeFrame);
    updateFollowButton(el);
    updateActiveJumpTurn(el);
    return () => {
      if (resizeFrame.current !== null) {
        cancelAnimationFrame(resizeFrame.current);
        resizeFrame.current = null;
      }
    };
  }, [footerHeight, updateActiveJumpTurn, updateFollowButton]);

  // Sub-agent calls carry a parentId; collect them under their parent `task`
  // call so the parent card can render them nested, and skip them at top level.
  const backgroundJobs = useMemo(() => jobs.filter((job) => job.status === "running"), [jobs]);
  // Layer state
  const [expandedWarmTurns, setExpandedWarmTurns] = useState<Set<number>>(new Set());
  const [coldPage, setColdPage] = useState(0);
  // Compute turn groups (memoized; only rebuilds when user turns change,
  // not on every streaming token). The warm previews are static once built.

  // How many turns are in the cold zone (not yet shown).
  const warmTurnCount = turnGroups.length - Math.min(turnGroups.length, HOT_TURNS);
  const shownWarmStart = visibleWarmStart(warmTurnCount, coldPage, WARM_PAGE_SIZE);
  const coldTurnCount = shownWarmStart;
  // The turn action menu
  const [openAction, setOpenAction] = useState<OpenTurnAction | null>(null);
  useEffect(() => {
    if (openAction === null) return;
    const onDown = (e: MouseEvent) => {
      const el = e.target as Element | null;
      if (!el || !el.closest(".turn-actions")) setOpenAction(null);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [openAction]);

  const userTurn = useMemo(() => new Map(questions.map((question) => [question.id, question.turn])), [questions]);
  const checkpointsByTurn = useMemo(() => new Map(checkpoints.map((checkpoint) => [checkpoint.turn, checkpoint])), [checkpoints]);
  // JumpBar integration
  const jumpToQuestion = useCallback((question: QuestionAnchor): boolean => {
    const el = scrollRef.current;
    const node = document.getElementById(questionAnchorId(question.id));
    if (!el || !node) return false;
    stick.current = false;
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    const scrollerRect = el.getBoundingClientRect();
    const nodeRect = node.getBoundingClientRect();
    const top = el.scrollTop + nodeRect.top - scrollerRect.top - 12;
    viewportAnchor.current = null;
    el.scrollTop = Math.max(0, top);
    captureViewportAnchor(el);
    updateFollowButton(el);
    updateActiveJumpTurn(el);
    return true;
  }, [captureViewportAnchor, updateActiveJumpTurn, updateFollowButton]);

  const handleJumpToQuestion = useCallback((question: QuestionAnchor) => {
    const warmTurnStart = turnGroups.length - HOT_TURNS;
    if (question.turn < warmTurnStart) {
      pendingQuestionJump.current = question;
      const requiredPage = requiredWarmPage(warmTurnCount, question.turn, WARM_PAGE_SIZE);
      if (requiredPage > coldPage) setColdPage(requiredPage);
      setExpandedWarmTurns((prev) => {
        if (prev.has(question.turn)) return prev;
        return new Set([...prev, question.turn]);
      });
      return;
    }
    jumpToQuestion(question);
  }, [coldPage, jumpToQuestion, turnGroups.length, warmTurnCount]);

  useLayoutEffect(() => {
    const question = pendingQuestionJump.current;
    if (!question) return;
    if (jumpToQuestion(question)) pendingQuestionJump.current = null;
  }, [coldPage, expandedWarmTurns, jumpToQuestion]);
  // Hot zone: fully rendered from hotStartIdx to end
  // Memoized separately from the assembly so streaming tokens don't rebuild
  // the warm/cold zone JSX trees. Uses LiveStreamContext for streaming data
  // (added by upstream PR #3423) instead of per-call renderSegments.
  const empty = items.length === 0;
  const hotItems = useMemo(() => items.slice(hotStartIdx), [hotStartIdx, items]);
  // Assemble rendered output
  // Warm/cold zone is a separate memo'd WarmZone component so streaming tokens
  // don't rebuild it. The hot zone uses LiveAssistantMessage (reads live from
  // LiveStreamContext) so streaming updates are captured immediately.
  return (
    <div className="transcript-shell">
      {!empty && showQuestionNav && (
        <QuestionJumpBar questions={questions} activeTurn={activeJumpTurn} onJump={handleJumpToQuestion} />
      )}
      <div
        className={`transcript${empty ? " transcript--empty" : ""}${hydrating ? " transcript--hydrating" : ""}`}
        ref={scrollRef}
        onScroll={onScroll}
        onWheel={(event) => { if (event.deltaY < 0 || !isAtBottom(event.currentTarget)) leaveFollowMode(); }}
        onPointerDown={leaveFollowMode}
        onTouchStart={leaveFollowMode}
        onKeyDown={(event) => {
          if (["ArrowUp", "PageUp", "Home"].includes(event.key)) leaveFollowMode();
        }}
        tabIndex={0}
      >
        <div className="transcript-content" ref={contentRef}>
          {empty && <Welcome onPrompt={onPrompt} />}

          <LiveStreamContext.Provider value={live}>
            {turnGroups.length > HOT_TURNS && (
              <WarmZone
                turnGroups={turnGroups}
                expandedWarmTurns={expandedWarmTurns}
                shownWarmStart={shownWarmStart}
                coldTurnCount={coldTurnCount}
                scrollRef={scrollRef}
                warmItems={items}
                warmSubcalls={subcallsByParent}
                warmUserTurn={userTurn}
                warmCheckpoints={checkpointsByTurn}
                warmOpenAction={openAction}
                warmActionPending={actionPending}
                warmRewindDisabled={rewindDisabled}
                warmOnRewind={onRewind}
                warmOnEditUserMessage={onEditUserMessage}
                warmSetOpenAction={setOpenAction}
                processDisplayMode={processDisplayMode}
                liveToolID={liveToolID}
                onToggleColdPage={() => setColdPage((p) => p + 1)}
                onToggleWarmTurn={(g, expand) => {
                  setExpandedWarmTurns((prev) => {
                    const next = new Set(prev);
                    if (expand) next.add(g); else next.delete(g);
                    return next;
                  });
                }}
              />
            )}
            <TimelineItems
              items={hotItems}
              running={running}
              processDisplayMode={processDisplayMode}
              subcalls={subcallsByParent}
              liveToolID={liveToolID}
              userTurnMap={userTurn}
              checkpoints={checkpointsByTurn}
              openAction={openAction}
              actionPending={actionPending}
              rewindDisabled={rewindDisabled}
              onRewind={onRewind}
              onEditUserMessage={onEditUserMessage}
              setOpenAction={setOpenAction}
              activityIndicatorEnabled={activityIndicatorEnabled}
              paused={paused}
            />
            {backgroundJobs.length > 0 && <BackgroundJobsCard jobs={backgroundJobs} />}
          </LiveStreamContext.Provider>
        </div>
      </div>

      {!empty && followButton && showFollowButton && (
        <button
          type="button"
          className="transcript-follow"
          aria-label={t("transcript.followLatest")}
          title={t("transcript.followLatest")}
          onClick={() => scrollToBottom(true)}
        >
          <ArrowDown size={18} aria-hidden="true" />
        </button>
      )}
    </div>
  );
}

// WarmZone sub-component (React.memo for streaming isolation)
// Receives structural props only; reads streaming state (items, live) via refs
// so it never invalidates on streaming token arrival.

const WarmZone = memo(function WarmZone({
  turnGroups,
  expandedWarmTurns,
  shownWarmStart,
  coldTurnCount,
  scrollRef,
  warmItems,
  warmSubcalls,
  warmUserTurn,
  warmCheckpoints,
  warmOpenAction,
  warmActionPending,
  warmRewindDisabled,
  warmOnRewind,
  warmOnEditUserMessage,
  warmSetOpenAction,
  processDisplayMode = "compact",
  liveToolID = "",
  onToggleColdPage,
  onToggleWarmTurn,
}: {
  turnGroups: TurnGroup[];
  expandedWarmTurns: ReadonlySet<number>;
  shownWarmStart: number;
  coldTurnCount: number;
  scrollRef: React.RefObject<HTMLDivElement | null>;
  warmItems: readonly Item[];
  warmSubcalls: ReadonlyMap<string, ToolItem[]>;
  warmUserTurn: ReadonlyMap<string, number>;
  warmCheckpoints: ReadonlyMap<number, CheckpointMeta>;
  warmOpenAction: OpenTurnAction | null;
  warmActionPending: boolean;
  warmRewindDisabled: boolean;
  warmOnRewind: ((turn: number, scope: string) => void) | undefined;
  warmOnEditUserMessage?: (text: string) => void;
  warmSetOpenAction: (action: OpenTurnAction | null) => void;
  processDisplayMode?: ProcessDisplayMode;
  liveToolID?: string;
  onToggleColdPage: () => void;
  onToggleWarmTurn: (g: number, expand: boolean) => void;
}) {
  const t = useT();
  const out: React.ReactNode[] = [];

  // 1. Cold zone: paginated warm turns (show more button).
  if (coldTurnCount > 0) {
    out.push(
      <button
        key="cold-load-more"
        type="button"
        className="warm-collapse"
        onClick={onToggleColdPage}
      >
        {t("transcript.showEarlierHistory", { n: coldTurnCount })}
      </button>,
    );
  }

  // 2. Warm zone: collapsed/expanded warm turn cards.
  let warmStartTurn = 0;
  if (turnGroups.length > HOT_TURNS) {
    warmStartTurn = shownWarmStart;
    for (let g = warmStartTurn; g < turnGroups.length - HOT_TURNS; g++) {
      const group = turnGroups[g];
      if (!group) continue;
      const expanded = expandedWarmTurns.has(g);

      if (expanded) {
        const userText = group.userItem.kind === "user" ? group.userItem.text : "";
        out.push(
          <WarmTurnCard
            key={`warm-${g}`}
            userText={warmUserPreview(userText)}
            assistantPreview={group.assistantPreview}
            toolCount={group.toolCount}
            expanded={true}
            onToggle={() => onToggleWarmTurn(g, false)}
          >
            {/* Expanded warm turns render items that are stable (never the
                streaming turn), so this captures items/live via a ref. */}
            <WarmTurnItems
              startIdx={group.startIdx}
              endIdx={group.endIdx}
              items={warmItems}
              subcalls={warmSubcalls}
              userTurnMap={warmUserTurn}
              checkpoints={warmCheckpoints}
              openAction={warmOpenAction}
              actionPending={warmActionPending}
              rewindDisabled={warmRewindDisabled}
              onRewind={warmOnRewind}
              onEditUserMessage={warmOnEditUserMessage}
              setOpenAction={warmSetOpenAction}
              processDisplayMode={processDisplayMode}
              liveToolID={liveToolID}
            />
          </WarmTurnCard>,
        );
      } else {
        const userText = group.userItem.kind === "user" ? group.userItem.text : "";
        out.push(
          <WarmTurnCard
            key={`warm-${g}`}
            userText={warmUserPreview(userText)}
            assistantPreview={group.assistantPreview}
            toolCount={group.toolCount}
            expanded={false}
            onToggle={() => {
              onToggleWarmTurn(g, true);
              const el = scrollRef.current;
              const node = document.getElementById(questionAnchorId(group.userItem.id));
              if (el && node) {
                requestAnimationFrame(() => {
                  el.scrollTo({ top: node.offsetTop - el.offsetTop - 80, behavior: "smooth" });
                });
              }
            }}
          />,
        );
      }
    }
  }

  return out;
});

function WarmTurnItems({
  startIdx,
  endIdx,
  items,
  subcalls,
  userTurnMap,
  checkpoints,
  openAction,
  actionPending,
  rewindDisabled,
  onRewind,
  onEditUserMessage,
  setOpenAction,
  processDisplayMode = "compact",
  liveToolID = "",
}: {
  startIdx: number;
  endIdx: number;
  items: readonly Item[];
  subcalls: ReadonlyMap<string, ToolItem[]>;
  userTurnMap: ReadonlyMap<string, number>;
  checkpoints: ReadonlyMap<number, CheckpointMeta>;
  openAction: OpenTurnAction | null;
  actionPending: boolean;
  rewindDisabled: boolean;
  onRewind: ((turn: number, scope: string) => void) | undefined;
  onEditUserMessage?: (text: string) => void;
  setOpenAction: (action: OpenTurnAction | null) => void;
  processDisplayMode?: ProcessDisplayMode;
  liveToolID?: string;
}) {
  const turnItems = items.slice(startIdx, Math.min(endIdx, items.length));
  return (
    <TimelineItems
      items={turnItems}
      running={false}
      processDisplayMode={processDisplayMode}
      subcalls={subcalls}
      liveToolID={liveToolID}
      userTurnMap={userTurnMap}
      checkpoints={checkpoints}
      openAction={openAction}
      actionPending={actionPending}
      rewindDisabled={rewindDisabled}
      onRewind={onRewind}
      onEditUserMessage={onEditUserMessage}
      setOpenAction={setOpenAction}
    />
  );
}

// Warm turn summary card

function WarmTurnCard({
  userText,
  assistantPreview,
  toolCount,
  expanded,
  onToggle,
  children,
}: {
  userText: string;
  assistantPreview: string;
  toolCount: number;
  expanded: boolean;
  onToggle: () => void;
  children?: React.ReactNode;
}) {
  const t = useT();
  return (
    <div className={`warm-turn${expanded ? " warm-turn--expanded" : ""}`}>
      <button
        type="button"
        className="warm-turn__head"
        onClick={onToggle}
        aria-expanded={expanded}
      >
        <span className="warm-turn__chevron">
          <ChevronRight className={expanded ? "warm-turn__chevron--open" : ""} size={13} />
        </span>
        <span className="warm-turn__preview">{userText}</span>
        <span className="warm-turn__meta">
          {toolCount > 0 && <span>{t("transcript.toolCount", { n: toolCount })}</span>}
        </span>
      </button>
      {expanded ? (
        <div className="warm-turn__body">{children}</div>
      ) : (
        assistantPreview && <div className="warm-turn__assistant">{assistantPreview}</div>
      )}
    </div>
  );
}

// JumpBar, PhaseCard, NoticeCard, CompactionCard

function QuestionJumpBar({
  questions,
  activeTurn,
  onJump,
}: {
  questions: QuestionAnchor[];
  activeTurn: number | null;
  onJump: (question: QuestionAnchor) => void;
}) {
  const t = useT();
  const [hovered, setHovered] = useState<number | null>(null);
  const barRef = useRef<HTMLDivElement>(null);
  const previewTop = useRef(0);
  const [showPreview, setShowPreview] = useState(false);

  useEffect(() => {
    if (activeTurn === null) return;
    const rail = barRef.current?.querySelector<HTMLElement>(".jump-scroll");
    const el = rail?.querySelector<HTMLElement>(`[data-turn="${activeTurn}"]`);
    if (!rail || !el) return;
    const top = el.offsetTop;
    const bottom = top + el.offsetHeight;
    if (top < rail.scrollTop) rail.scrollTop = top;
    else if (bottom > rail.scrollTop + rail.clientHeight) rail.scrollTop = bottom - rail.clientHeight;
  }, [activeTurn]);

  const hoverIdx = hovered !== null ? questions.findIndex((question) => question.turn === hovered) : -1;
  const hoveredQuestion = hovered !== null ? questions.find((question) => question.turn === hovered) : undefined;

  const closestQuestionFromY = (clientY: number): { question: QuestionAnchor; previewY: number } | null => {
    const el = barRef.current;
    if (!el) return null;
    const markers = el.querySelectorAll<HTMLElement>(".jump-item");
    const barRect = el.getBoundingClientRect();
    let closest = -1;
    let closestDist = Infinity;
    let closestY = 0;
    markers.forEach((item, index) => {
      const rect = item.getBoundingClientRect();
      const midY = rect.top + rect.height / 2;
      const dist = Math.abs(clientY - midY);
      if (dist < closestDist) {
        closestDist = dist;
        closest = index;
        closestY = midY - barRect.top;
      }
    });
    const question = questions[closest];
    if (!question) return null;
    return { question, previewY: closestY };
  };

  const onMove = (e: ReactMouseEvent<HTMLDivElement>) => {
    const closest = closestQuestionFromY(e.clientY);
    if (!closest) return;
    previewTop.current = closest.previewY;
    setHovered(closest.question.turn);
    setShowPreview(true);
  };

  const scrollTo = (question: QuestionAnchor) => {
    onJump(question);
  };

  const onRailMouseDown = (e: ReactMouseEvent<HTMLDivElement>) => {
    const closest = closestQuestionFromY(e.clientY);
    if (!closest) return;
    e.preventDefault();
    previewTop.current = closest.previewY;
    setHovered(closest.question.turn);
    setShowPreview(true);
    scrollTo(closest.question);
  };

  const onItemMouseDown = (e: ReactMouseEvent<HTMLButtonElement>, question: QuestionAnchor) => {
    e.preventDefault();
    scrollTo(question);
  };

  const dotProps = (
    idx: number,
    turn: number,
  ): { style: CSSProperties; "data-d"?: string } => {
    const isActive = activeTurn === turn;
    if (hoverIdx < 0) {
      return { style: { width: isActive ? 18 : 12, background: isActive ? "var(--accent)" : undefined } };
    }
    const d = Math.abs(idx - hoverIdx);
    const width = d === 0 ? 32 : d === 1 ? 20 : d === 2 ? 14 : isActive ? 18 : 12;
    const background = d <= 2 ? undefined : isActive ? "var(--accent)" : undefined;
    return {
      style: { width, transitionDelay: `${d * 20}ms`, background },
      "data-d": d <= 2 ? String(d) : undefined,
    };
  };

  return (
    <nav
      className="jump-bar"
      ref={barRef}
      aria-label={t("questionNav.label")}
      onMouseMove={onMove}
      onMouseLeave={() => {
        setHovered(null);
        setShowPreview(false);
      }}
    >
      <div className="jump-scroll" onMouseDown={onRailMouseDown}>
        {questions.map((question, index) => (
          <button
            className="jump-item"
            key={question.id}
            type="button"
            data-turn={question.turn}
            aria-label={t("questionNav.jump", { n: question.turn + 1 })}
            onMouseDown={(e) => {
              e.stopPropagation();
              onItemMouseDown(e, question);
            }}
            onClick={(e) => {
              e.stopPropagation();
              if (e.detail === 0) scrollTo(question);
            }}
          >
            <span className="jump-dot" {...dotProps(index, question.turn)} />
          </button>
        ))}
      </div>
      {showPreview && hoveredQuestion && (
        <div className="jump-preview" style={{ top: previewTop.current }} role="tooltip">
          <span className="jump-text">{hoveredQuestion.text}</span>
        </div>
      )}
    </nav>
  );
}

type CompactionItem = Extract<Item, { kind: "compaction" }>;
type NoticeItem = Extract<Item, { kind: "notice" }>;

function PhaseCard({ text }: { text: string }) {
  return (
    <ProcessCard
      tone="accent"
      icon={<ProcessPhaseIcon size={12} />}
      kind="phase"
      name={text}
      className="phase process-card--phase"
    />
  );
}

function NoticeCard({ level, text }: { level: NoticeItem["level"]; text: string }) {
  const t = useT();
  const warning = level === "warn";
  return (
    <ProcessCard
      tone={warning ? "warning" : "default"}
      icon={<ProcessInfoIcon size={12} />}
      kind="notice"
      name={t(warning ? "notice.warning" : "notice.info")}
      meta={warning ? <ProcessStatusIcon state="waiting" label={t("notice.warning")} /> : undefined}
      className={`notice notice--${level}`}
    >
      <div className="notice__body">{text}</div>
    </ProcessCard>
  );
}

function SteerCard({ text }: { text: string }) {
  const t = useT();
  return (
    <ProcessCard
      tone="accent"
      icon={<ChevronRight size={12} />}
      kind={t("steer.kind")}
      name={t("steer.title")}
      defaultOpen
      className="steer"
    >
      <div className="steer__body">{text}</div>
    </ProcessCard>
  );
}

function CompactionCard({ item }: { item: CompactionItem }) {
  const t = useT();
  const trigger = item.trigger === "manual" ? "manual" : item.trigger === "auto" ? "auto" : item.trigger;
  if (item.pending) {
    return (
      <ProcessCard
        tone="accent"
        icon={<ProcessCompactIcon size={12} />}
        kind="context"
        name={t("compaction.working")}
        meta={<ProcessStatusIcon state="running" label={t("compaction.working")} />}
        className="compaction compaction--pending"
      />
    );
  }
  return (
    <ProcessCard
      tone="accent"
      icon={<ProcessCompactIcon size={12} />}
      kind="context"
      name={t("compaction.title")}
      meta={`${t("compaction.messages", { n: item.messages })}${trigger ? ` - ${trigger}` : ""}`}
      className="compaction"
    >
      <pre className="compaction__summary">{item.summary}</pre>
    </ProcessCard>
  );
}
