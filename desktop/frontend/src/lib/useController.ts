// useController is the frontend's state machine over the agent's event stream. It
// maintains per-tab state so background tabs preserve their streaming output, tool
// states, and approvals when the user switches away and back. The active tab's state
// is what components render.

import { useCallback, useEffect, useRef, useState } from "react";
import { asArray } from "./array";
import { app, onEvent, onReady, onRuntimeSwitchProgress } from "./bridge";
import { createRafBatch } from "./rafBatch";
import { t } from "./i18n";
import { modeHasAutoApproveTools } from "./types";
import type {
  BalanceInfo,
  CheckpointMeta,
  CollaborationMode,
  ContextInfo,
  EffortInfo,
  HistoryMessage,
  JobView,
  MemoryView,
  Meta,
  Mode,
  QuestionAnswer,
  PromptMode,
  RuntimeSwitchPhase,
  RuntimeSwitchProgress,
  SessionMeta,
  TabMeta,
  ToolApprovalMode,
  TurnItemStatus,
  TurnOutcome,
  WireApproval,
  WireAsk,
  WireEvent,
  WireUsage,
} from "./types";

export type ToolStatus = "running" | "done" | "error" | "stopped";

export type LiveStream = { id: string; text: string; reasoning: string };
export type MessageActionScope = "fork" | "summ-from" | "summ-upto" | "conversation" | "code" | "both";
export type MessageActionState = { turn: number; scope: MessageActionScope };

type ItemLifecycle = {
  turnId?: string;
  itemId?: string;
  messageId?: string;
  itemStatus?: TurnItemStatus;
  final?: boolean;
};

export type Item = ItemLifecycle & (
  | { kind: "user"; id: string; text: string; failed?: boolean }
  | { kind: "assistant"; id: string; text: string; reasoning: string; streaming: boolean }
  | { kind: "steer"; id: string; text: string }
  | { kind: "phase"; id: string; text: string }
  | { kind: "notice"; id: string; level: "info" | "warn"; text: string }
  | {
      kind: "mode_switch";
      id: string;
      fromMode: PromptMode;
      toMode: PromptMode;
      appliedMode?: PromptMode;
      phase: RuntimeSwitchPhase;
      progress: number;
      startedAt: number;
      completedAt?: number;
      error?: string;
    }
  | { kind: "turn_stats"; id: string; elapsedMs?: number; tokens?: number; success: boolean; outcome?: TurnOutcome; finalMessageId?: string }
  | {
      kind: "compaction";
      id: string;
      pending: boolean;
      trigger: string;
      messages: number;
      summary: string;
      archive: string;
    }
  | {
      kind: "tool";
      id: string;
      name: string;
      args: string;
      readOnly: boolean;
      status: ToolStatus;
      output?: string;
      error?: string;
      truncated?: boolean;
      durationMs?: number;
      isShell?: boolean; // true for !-prefix shell commands (controls default expand)
      parentId?: string; // a sub-agent call nests under the `task` call with this id
      profile?: { model?: string; effort?: string }; // subagent model/effort from tool event
    });

const LEADING_REASONING_BLOCK_RE = /^\s*(?:\ufeff\s*)?<(think|thinking|reasoning)>\s*([\s\S]*?)\s*<\/\1>\s*/i;

function splitLeadingReasoningBlock(text: string): { reasoning: string; text: string } | null {
  const match = text.match(LEADING_REASONING_BLOCK_RE);
  if (!match) return null;
  return { reasoning: match[2] ?? "", text: text.slice(match[0].length) };
}

interface State {
  items: Item[];
  loading: boolean;
  hydrating: boolean;
  running: boolean;
  cancelRequested: boolean;
  turnActive: boolean;
  approval?: WireApproval;
  ask?: WireAsk;
  usage?: WireUsage;
  context: ContextInfo;
  meta?: Meta;
  balance?: BalanceInfo;
  effort?: EffortInfo;
  jobs: JobView[];
  checkpoints: CheckpointMeta[];
  messageAction?: MessageActionState;
  currentAssistant?: string;
  currentTurnId?: string;
  committedFinalMessageId?: string;
  live?: LiveStream;
  pendingUser?: string;
  discardTurn?: boolean;
  turnStartAt: number;
  turnProcessStartAt: number;
  turnTokens: number;
  turnUsageTokens: number;
  sessionTokens: number;
  sessionCost: number;
  sessionCurrency: string;
  costAvailable: boolean;
  retry?: { attempt: number; max: number };
  runtimeSwitch?: RuntimeSwitchProgress;
  seq: number;
}

export const initialState: State = {
  items: [],
  loading: false,
  hydrating: false,
  running: false,
  cancelRequested: false,
  turnActive: false,
  context: { used: 0, window: 0, sessionTokens: 0, windowConfirmed: false, costAvailable: false },
  jobs: [],
  checkpoints: [],
  turnStartAt: 0,
  turnProcessStartAt: 0,
  turnTokens: 0,
  turnUsageTokens: 0,
  sessionTokens: 0,
  sessionCost: 0,
  sessionCurrency: "",
  costAvailable: false,
  seq: 0,
};

function usageFromContext(context: State["context"], fallback?: WireUsage): WireUsage | undefined {
  const sessionHit = context.sessionCacheHitTokens ?? fallback?.sessionCacheHitTokens ?? 0;
  const sessionMiss = context.sessionCacheMissTokens ?? fallback?.sessionCacheMissTokens ?? 0;
  const promptTokens = context.promptTokens ?? fallback?.promptTokens ?? context.sessionPromptTokens ?? 0;
  const completionTokens = context.completionTokens ?? fallback?.completionTokens ?? 0;
  const totalTokens = context.totalTokens ?? fallback?.totalTokens ?? Math.max(0, promptTokens + completionTokens);
  const cacheHitTokens = context.cacheHitTokens ?? fallback?.cacheHitTokens ?? sessionHit;
  const cacheMissTokens = context.cacheMissTokens ?? fallback?.cacheMissTokens ?? sessionMiss;
  const reasoningTokens = context.reasoningTokens ?? fallback?.reasoningTokens ?? 0;
  const costAvailable = context.costAvailable === true;
  const sanitizeCost = (usage: WireUsage): WireUsage => costAvailable
    ? { ...usage, costAvailable: true, currency: context.sessionCurrency || usage.currency }
    : { ...usage, cost: undefined, costUsd: undefined, currency: undefined, costAvailable: false };
  if (promptTokens + completionTokens + totalTokens + cacheHitTokens + cacheMissTokens + reasoningTokens + sessionHit + sessionMiss <= 0) {
    return fallback ? sanitizeCost(fallback) : undefined;
  }
  return {
    promptTokens,
    completionTokens,
    totalTokens,
    cacheHitTokens,
    cacheMissTokens,
    reasoningTokens,
    sessionCacheHitTokens: sessionHit,
    sessionCacheMissTokens: sessionMiss,
    cost: costAvailable ? fallback?.cost : undefined,
    costAvailable,
    costUsd: costAvailable ? fallback?.costUsd : undefined,
    currency: costAvailable ? (context.sessionCurrency || fallback?.currency) : undefined,
  };
}

function sameMeta(a?: Meta, b?: Meta): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return (
    a.label === b.label &&
    a.ready === b.ready &&
    a.startupErr === b.startupErr &&
    a.eventChannel === b.eventChannel &&
    a.cwd === b.cwd &&
    a.autoApproveTools === b.autoApproveTools &&
    a.bypass === b.bypass &&
    a.toolApprovalMode === b.toolApprovalMode &&
    a.askWorkflowEnabled === b.askWorkflowEnabled &&
    a.stepThinkingEnabled === b.stepThinkingEnabled &&
    a.promptMode === b.promptMode &&
    a.enhancedModeEnabled === b.enhancedModeEnabled &&
    a.paused === b.paused &&
    a.goal === b.goal &&
    a.goalStatus === b.goalStatus
  );
}

type Action =
  | { type: "event"; e: WireEvent }
  | { type: "user"; text: string; seq: number }
  | { type: "unsend" }
  | { type: "cancel_requested" }
  | { type: "cancel_rejected" }
  | { type: "send_failed"; error: string }
  | { type: "backend_status"; running: boolean }
  | { type: "meta"; meta: Meta }
  | { type: "context"; context: ContextInfo }
  | { type: "balance"; balance: BalanceInfo }
  | { type: "effort"; effort: EffortInfo }
  | { type: "jobs"; jobs: JobView[] }
  | { type: "checkpoints"; checkpoints: CheckpointMeta[] }
  | { type: "message_action_start"; action: MessageActionState }
  | { type: "message_action_done" }
  | { type: "history"; messages: HistoryMessage[] }
  | { type: "session_load_start"; hydrating: boolean }
  | { type: "session_primary_loaded" }
  | { type: "session_hydrated" }
  | { type: "local_notice"; level: "info" | "warn"; text: string }
  | { type: "runtime_switch"; progress: RuntimeSwitchProgress }
  | { type: "steer_sent"; text: string }
  | { type: "clearApproval" }
  | { type: "clearAsk" }
  | { type: "reset" };

// ---- reducer helpers (unchanged logic) ----

export function historyMessagesToItems(messages: HistoryMessage[], idPrefix: string, startSeq = 0): { items: Item[]; seq: number } {
  const resultByID = new Map<string, HistoryMessage>();
  for (const m of messages) {
    if (m.role === "tool" && m.toolCallId && !resultByID.has(m.toolCallId)) {
      resultByID.set(m.toolCallId, m);
    }
  }

  const items: Item[] = [];
  let seq = startSeq;
  const consumedToolIDs = new Set<string>();
  for (const m of messages) {
    if (m.role === "system") continue;
    if (m.role === "mode_switch" && m.switchId && m.switchFromMode && m.switchToMode && m.switchPhase) {
      items.push({
        kind: "mode_switch",
        id: m.switchId,
        fromMode: m.switchFromMode,
        toMode: m.switchToMode,
        appliedMode: m.switchAppliedMode,
        phase: m.switchPhase,
        progress: m.switchProgress ?? 0,
        startedAt: m.switchStartedAt ?? 0,
        completedAt: m.switchCompletedAt,
        error: m.switchError,
      });
      seq++;
      continue;
    }
    if (m.role === "phase") {
      if (m.content.trim() !== "") {
        items.push({ kind: "phase", id: `${idPrefix}${seq}`, text: m.content });
        seq++;
      }
      continue;
    }
    if (m.role === "notice") {
      if (m.content.trim() !== "") {
        items.push({ kind: "notice", id: `${idPrefix}${seq}`, level: m.level === "warn" ? "warn" : "info", text: m.content });
        seq++;
      }
      continue;
    }
    if (m.role === "compaction") {
      items.push({
        kind: "compaction",
        id: `${idPrefix}${seq}`,
        pending: Boolean(m.pending),
        trigger: m.trigger ?? "",
        messages: m.messages ?? 0,
        summary: m.summary ?? "",
        archive: m.archive ?? "",
      });
      seq++;
      continue;
    }
    if (m.role === "turn_stats" && m.outcome) {
      items.push({
        kind: "turn_stats",
        id: `${idPrefix}${seq}`,
        elapsedMs: m.elapsedMs,
        tokens: m.tokens,
        success: m.outcome === "success",
        outcome: m.outcome,
        turnId: m.turnId,
        finalMessageId: m.finalMessageId,
      });
      seq++;
      continue;
    }
    if (m.role === "user") {
      if (m.content.trim() === "") continue;
      items.push({ kind: "user", id: `${idPrefix}${seq}`, text: m.content, turnId: m.turnId });
      seq++;
      continue;
    }
    if (m.role === "assistant") {
      let content = m.content;
      let reasoning = m.reasoning ?? "";
      if (reasoning.trim() === "") {
        const split = splitLeadingReasoningBlock(content);
        if (split) {
          content = split.text;
          reasoning = split.reasoning;
        }
      }
      const hasText = content.trim() !== "" || reasoning.trim() !== "";
      if (hasText) {
        items.push({
          kind: "assistant",
          id: m.messageId || `${idPrefix}${seq}`,
          text: content,
          reasoning,
          streaming: false,
          turnId: m.turnId,
          itemId: m.itemId,
          messageId: m.messageId,
          itemStatus: "completed",
          final: Boolean(m.final),
        });
        seq++;
      }
      for (const tc of m.toolCalls ?? []) {
        const result = resultByID.get(tc.id);
        if (tc.id) consumedToolIDs.add(tc.id);
        const output = result?.content ?? "";
        const error = output.startsWith("[error") || output.startsWith("Error:") ? output : undefined;
        items.push({
          kind: "tool",
          id: tc.id || `${idPrefix}tool${seq}`,
          name: tc.name,
          args: tc.arguments ?? "",
          readOnly: false,
          status: error ? "error" : "done",
          output,
          error,
          isShell: (tc.id || "").startsWith("shell-"),
        });
        seq++;
      }
      continue;
    }
    if (m.role === "tool") {
      if (m.toolCallId && consumedToolIDs.has(m.toolCallId)) continue;
      const output = m.content;
      const error = output.startsWith("[error") || output.startsWith("Error:") ? output : undefined;
      items.push({
        kind: "tool",
        id: m.toolCallId || `${idPrefix}tool${seq}`,
        name: m.toolName || "tool",
        args: "",
        readOnly: false,
        status: error ? "error" : "done",
        output,
        error,
        isShell: (m.toolCallId || "").startsWith("shell-"),
      });
      seq++;
      continue;
    }
  }
  return { items, seq };
}

function ensureAssistant(s: State, e?: WireEvent): { items: Item[]; id: string; seq: number } {
  const eventID = e?.messageId || e?.itemId;
  if (eventID) {
    const existing = s.items.find((it) => it.kind === "assistant" && (it.messageId === eventID || it.itemId === e.itemId));
    if (existing) return { items: s.items, id: existing.id, seq: s.seq };
    const item: Item = {
      kind: "assistant",
      id: eventID,
      text: "",
      reasoning: "",
      streaming: true,
      turnId: e.turnId || s.currentTurnId,
      itemId: e.itemId,
      messageId: e.messageId,
      itemStatus: e.itemStatus,
    };
    return { items: [...s.items, item], id: eventID, seq: s.seq };
  }
  if (s.currentAssistant) {
    const exists = s.items.some((it) => it.id === s.currentAssistant && it.kind === "assistant");
    if (exists) return { items: s.items, id: s.currentAssistant, seq: s.seq };
  }
  const id = `a${s.seq}`;
  const item: Item = { kind: "assistant", id, text: "", reasoning: "", streaming: true };
  return { items: [...s.items, item], id, seq: s.seq + 1 };
}

function flushPendingUser(s: State): State {
  if (s.pendingUser === undefined) return s;
  const lastItem = s.items[s.items.length - 1];
  if (lastItem?.kind === "user" && lastItem.text === s.pendingUser) {
    return { ...s, pendingUser: undefined };
  }
  return {
    ...s,
    seq: s.seq + 1,
    items: [...s.items, { kind: "user", id: `u${s.seq}`, text: s.pendingUser }],
    pendingUser: undefined,
  };
}

function usageTotalTokens(usage?: WireUsage): number {
  if (!usage) return 0;
  if (usage.totalTokens > 0) return usage.totalTokens;
  return Math.max(0, (usage.promptTokens ?? 0) + (usage.completionTokens ?? 0));
}

function hasCurrentTurnActivity(items: Item[]): boolean {
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.kind === "user") return false;
    if (it.kind === "assistant" || it.kind === "tool" || it.kind === "notice" || it.kind === "phase" || it.kind === "compaction") return true;
  }
  return false;
}

function appendTurnStats(s: State, items: Item[], outcome: TurnOutcome, finalMessageId?: string, now = Date.now()): { items: Item[]; seq: number } {
  if (!s.turnStartAt || (!s.currentTurnId && !hasCurrentTurnActivity(items))) return { items, seq: s.seq };
  const elapsedMs = Math.max(0, now - s.turnStartAt);
  const stats: Item = {
    kind: "turn_stats",
    id: `ts${s.seq}`,
    elapsedMs,
    tokens: s.turnUsageTokens > 0 ? s.turnUsageTokens : undefined,
    success: outcome === "success",
    outcome,
    turnId: s.currentTurnId,
    finalMessageId,
  };
  return { items: [...items, stats], seq: s.seq + 1 };
}

function applyEvent(s: State, e: WireEvent): State {
  if (s.discardTurn) {
    if (e.kind === "turn_done") return { ...s, discardTurn: false, running: false, cancelRequested: false, turnActive: false, currentAssistant: undefined, live: undefined };
    return s;
  }
  if (s.pendingUser !== undefined && e.kind !== "turn_done") {
    s = flushPendingUser(s);
  }
  if (e.kind === "retrying") {
    return { ...s, retry: { attempt: e.retryAttempt ?? 0, max: e.retryMax ?? 0 } };
  }
  if (s.retry) s = { ...s, retry: undefined };
  switch (e.kind) {
    case "external_user": {
      const text = (e.text ?? "").trim();
      if (!text) return s;
      const lastItem = s.items[s.items.length - 1];
      if (lastItem?.kind === "user" && lastItem.text === text) {
        return { ...s, running: true, turnStartAt: Date.now(), turnProcessStartAt: 0, turnTokens: 0, turnUsageTokens: 0 };
      }
      return {
        ...s,
        seq: s.seq + 1,
        items: [...s.items, { kind: "user", id: `u${s.seq}`, text }],
        running: true,
        turnStartAt: Date.now(),
        turnProcessStartAt: 0,
        turnTokens: 0,
        turnUsageTokens: 0,
      };
    }
    case "session_updated":
      return s;
    case "turn_started": {
      // Flush the user message and pre-create an empty assistant bubble
      // immediately so the user sees their message + a blinking cursor the
      // instant the backend acknowledges the turn — no dead gap waiting for
      // the first text/reasoning token.
      let cur: State = s;
      if (cur.pendingUser !== undefined) cur = flushPendingUser(cur);
      const now = Date.now();
      const turnId = e.turnId || cur.currentTurnId || `turn-${cur.seq}`;
      let claimedUser = false;
      const items = [...cur.items].reverse().map((item) => {
        if (!claimedUser && item.kind === "user" && !item.turnId) {
          claimedUser = true;
          return { ...item, turnId };
        }
        return item;
      }).reverse();
      return {
        ...cur,
        items,
        currentTurnId: turnId,
        committedFinalMessageId: undefined,
        running: true,
        cancelRequested: false,
        turnActive: true,
        turnStartAt: cur.turnActive && cur.turnStartAt ? cur.turnStartAt : now,
        turnProcessStartAt: cur.turnActive ? cur.turnProcessStartAt : 0,
        turnTokens: cur.turnActive ? cur.turnTokens : 0,
        turnUsageTokens: cur.turnActive ? cur.turnUsageTokens : 0,
      };
    }
    case "text":
    case "reasoning": {
      const { items, id, seq } = ensureAssistant(s, e);
      const delta = e.text ?? e.reasoning ?? "";
      const base = s.live?.id === id ? s.live : { id, text: "", reasoning: "" };
      const live = e.kind === "text" ? { ...base, text: base.text + delta } : { ...base, reasoning: base.reasoning + delta };
      return { ...s, items, live, currentAssistant: id, seq, turnProcessStartAt: s.turnProcessStartAt || Date.now() };
    }
    case "message": {
      const { items, id, seq } = ensureAssistant(s, e);
      const next = items.map((it) =>
        it.kind === "assistant" && it.id === id
          ? { ...it, text: e.text ?? s.live?.text ?? it.text, reasoning: e.reasoning ?? s.live?.reasoning ?? it.reasoning, streaming: false, turnId: e.turnId || it.turnId, itemId: e.itemId || it.itemId, messageId: e.messageId || it.messageId, itemStatus: "completed" as const }
          : it,
      );
      return { ...s, items: next, live: undefined, currentAssistant: undefined, seq };
    }
    case "answer_committed": {
      const finalMessageId = e.finalMessageId || e.messageId;
      const finalItemId = e.finalItemId || e.itemId;
      let fallbackID = "";
      if (!finalMessageId && !finalItemId) {
        for (let index = s.items.length - 1; index >= 0; index -= 1) {
          if (s.items[index].kind === "assistant") {
            fallbackID = s.items[index].id;
            break;
          }
        }
      }
      const next = s.items.map((it) => it.kind === "assistant" && (
        (finalMessageId && it.messageId === finalMessageId) ||
        (finalItemId && it.itemId === finalItemId) ||
        (fallbackID && it.id === fallbackID)
      ) ? { ...it, final: true } : it);
      return { ...s, items: next, committedFinalMessageId: finalMessageId || s.committedFinalMessageId };
    }
    case "tool_dispatch": {
      const t = e.tool;
      if (!t) return s;
      const id = t.id || `tool${s.seq}`;
      const idx = s.items.findIndex((it) => it.kind === "tool" && it.id === id);
      if (idx >= 0) {
        const next = [...s.items];
        const it = next[idx];
        if (it.kind === "tool") next[idx] = { ...it, name: t.name, args: t.args ? t.args : it.args, readOnly: t.readOnly, profile: t.profile ?? it.profile, turnId: e.turnId || it.turnId, itemId: e.itemId || it.itemId, itemStatus: e.itemStatus || it.itemStatus };
        return { ...s, items: next };
      }
      return { ...s, seq: s.seq + 1, turnProcessStartAt: s.turnProcessStartAt || Date.now(), items: [...s.items, { kind: "tool", id, name: t.name, args: t.args ?? "", readOnly: t.readOnly, status: "running", isShell: id.startsWith("shell-"), parentId: t.parentId, profile: t.profile, turnId: e.turnId || s.currentTurnId, itemId: e.itemId, itemStatus: e.itemStatus }] };
    }
    case "tool_result": {
      const t = e.tool;
      if (!t) return s;
      const next = [...s.items];
      let idx = t.id ? next.findIndex((it) => it.kind === "tool" && it.id === t.id) : -1;
      if (idx < 0) {
        for (let i = next.length - 1; i >= 0; i--) {
          const it = next[i];
          if (it.kind === "tool" && it.status === "running") { idx = i; break; }
        }
      }
      if (idx >= 0) {
        const it = next[idx];
        if (it.kind === "tool") next[idx] = { ...it, status: t.err ? "error" : "done", output: t.output, error: t.err, truncated: t.truncated, durationMs: t.durationMs, turnId: e.turnId || it.turnId, itemId: e.itemId || it.itemId, itemStatus: e.itemStatus || (t.err ? "failed" : "completed") };
      }
      return { ...s, items: next };
    }
    case "tool_progress": {
      const t = e.tool;
      if (!t?.id) return s;
      const idx = s.items.findIndex((it) => it.kind === "tool" && it.id === t.id);
      if (idx < 0) return s;
      const next = [...s.items];
      const it = next[idx];
      if (it.kind === "tool") next[idx] = { ...it, output: (it.output ?? "") + (t.output ?? ""), turnId: e.turnId || it.turnId, itemId: e.itemId || it.itemId, itemStatus: e.itemStatus || "streaming" };
      return { ...s, items: next };
    }
    case "usage": {
      const turnTokens = s.turnTokens + (e.usage?.completionTokens ?? 0);
      const turnUsageTokens = s.turnUsageTokens + usageTotalTokens(e.usage);
      const sessionTokens = Math.max(0, s.context.sessionTokens ?? s.sessionTokens, s.sessionTokens);
      const costAvailable = e.usage?.costAvailable ?? (typeof e.usage?.cost === "number" && Boolean(e.usage?.currency));
      const sessionCurrency = costAvailable ? (e.usage?.currency || s.sessionCurrency) : "";
      return { ...s, usage: e.usage, context: { ...s.context, sessionTokens, costAvailable }, turnTokens, turnUsageTokens, sessionTokens, sessionCurrency, costAvailable };
    }
    case "notice":
      return { ...s, running: s.turnActive ? s.running : false, turnProcessStartAt: s.turnProcessStartAt || Date.now(), seq: s.seq + 1, items: [...s.items, { kind: "notice", id: e.itemId || `n${s.seq}`, level: e.level ?? "info", text: e.text ?? "", turnId: e.turnId || s.currentTurnId, itemId: e.itemId, itemStatus: e.itemStatus }] };
    case "phase":
      return { ...s, turnProcessStartAt: s.turnProcessStartAt || Date.now(), seq: s.seq + 1, items: [...s.items, { kind: "phase", id: e.itemId || `p${s.seq}`, text: e.text ?? "", turnId: e.turnId || s.currentTurnId, itemId: e.itemId, itemStatus: e.itemStatus }] };
    case "compaction_started":
      return { ...s, turnProcessStartAt: s.turnProcessStartAt || Date.now(), seq: s.seq + 1, items: [...s.items, { kind: "compaction", id: e.itemId || `c${s.seq}`, pending: true, trigger: e.compaction?.trigger ?? "", messages: 0, summary: "", archive: "", turnId: e.turnId || s.currentTurnId, itemId: e.itemId, itemStatus: e.itemStatus }] };
    case "compaction_done": {
      const c = e.compaction;
      const idx = [...s.items].reverse().findIndex((it) => it.kind === "compaction" && it.pending);
      const at = idx < 0 ? -1 : s.items.length - 1 - idx;
      if (!c?.summary) {
        const items = at < 0 ? s.items : s.items.filter((_, i) => i !== at);
        return { ...s, running: s.turnActive ? s.running : false, items };
      }
      const previous = at < 0 ? undefined : s.items[at] as Extract<Item, { kind: "compaction" }>;
      const filled: Item = { kind: "compaction", id: previous?.id || e.itemId || `c${s.seq}`, pending: false, trigger: c.trigger ?? "", messages: c.messages ?? 0, summary: c.summary, archive: c.archive ?? "", turnId: e.turnId || previous?.turnId || s.currentTurnId, itemId: e.itemId || previous?.itemId, itemStatus: e.itemStatus || "completed" };
      const items = at < 0 ? [...s.items, filled] : s.items.map((it, i) => (i === at ? filled : it));
      return { ...s, running: s.turnActive ? s.running : false, seq: s.seq + 1, items };
    }
    case "steer": {
      const text = (e.text ?? "").trim();
      if (!text) return s;
      const existing = [...s.items].reverse().find((it) => it.kind === "steer" && it.text.trim() === text);
      if (existing) return s;
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "steer", id: `s${s.seq}`, text }] };
    }
    case "approval_request": return { ...s, approval: e.approval };
    case "ask_request": return { ...s, ask: e.ask };
    case "turn_done": {
      if (s.pendingUser !== undefined) s = flushPendingUser(s);
      const finalized = s.items.map((it) => {
        if (it.kind === "assistant" && s.live && it.id === s.live.id) return { ...it, text: s.live.text, reasoning: s.live.reasoning, streaming: false };
        if (it.kind === "assistant" && it.streaming) return { ...it, streaming: false };
        if (it.kind === "tool" && it.status === "running") return { ...it, status: "stopped" as const };
        return it;
      });
      let outcome: TurnOutcome = e.outcome ?? (e.err ? "failed" : "success");
      const finalMessageId = e.finalMessageId || s.committedFinalMessageId;
      const hasCommittedFinal = finalized.some((it) => it.kind === "assistant" && it.final && it.text.trim() !== "");
      if (outcome === "success" && e.turnId && !finalMessageId && !hasCommittedFinal) outcome = "interrupted";
      const showError = Boolean(e.err) && outcome !== "cancelled";
      const withError: Item[] = showError ? [...finalized, { kind: "notice", id: `e${s.seq}`, level: "warn", text: e.err!, turnId: e.turnId || s.currentTurnId }] : finalized;
      const stats = appendTurnStats(s, withError, outcome, finalMessageId);
      return { ...s, items: stats.items, live: undefined, running: false, cancelRequested: false, turnActive: false, currentAssistant: undefined, currentTurnId: undefined, committedFinalMessageId: undefined, approval: undefined, ask: undefined, seq: stats.seq, turnStartAt: 0, turnProcessStartAt: 0, turnTokens: 0, turnUsageTokens: 0 };
    }
    default: return s;
  }
}

export function reducer(s: State, a: Action): State {
  switch (a.type) {
    case "user": {
      const seq = a.seq !== undefined ? a.seq : s.seq;
      return {
        ...s,
        seq: seq + 1,
        items: [...s.items, { kind: "user", id: `u${seq}`, text: a.text }],
        running: true,
        turnStartAt: Date.now(),
        turnProcessStartAt: 0,
        turnTokens: 0,
        turnUsageTokens: 0,
        pendingUser: a.text,
        discardTurn: false,
      };
    }
    case "unsend": return { ...s, pendingUser: undefined, discardTurn: true, running: false, live: undefined };
    case "cancel_requested": return s.cancelRequested ? s : { ...s, cancelRequested: true };
    case "cancel_rejected": return s.cancelRequested ? { ...s, cancelRequested: false } : s;
    case "send_failed": {
      if (s.pendingUser === undefined) return s;
      let idx = -1;
      for (let i = s.items.length - 1; i >= 0; i--) {
        const it = s.items[i];
        if (it.kind === "user" && it.text === s.pendingUser) { idx = i; break; }
      }
      const items = idx >= 0 ? s.items.map((it, i) => (i === idx ? { ...it, failed: true } : it)) : s.items;
      const notice: Item = { kind: "notice", id: `n${s.seq}`, level: "warn", text: a.error };
      return { ...s, pendingUser: undefined, running: false, turnActive: false, live: undefined, seq: s.seq + 1, items: [...items, notice] };
    }
    case "backend_status": {
      if (a.running === s.running) return s;
      if (a.running) return { ...s, running: true, turnActive: true, turnStartAt: s.turnStartAt || Date.now() };
      const finalized = s.items.map((it) => {
        if (it.kind === "assistant" && s.live && it.id === s.live.id) return { ...it, text: s.live.text, reasoning: s.live.reasoning, streaming: false };
        if (it.kind === "assistant" && it.streaming) return { ...it, streaming: false };
        if (it.kind === "tool" && it.status === "running") return { ...it, status: "stopped" as const };
        return it;
      });
      const stats = appendTurnStats(s, finalized, "interrupted", s.committedFinalMessageId);
      return { ...s, items: stats.items, running: false, turnActive: false, live: undefined, currentAssistant: undefined, currentTurnId: undefined, committedFinalMessageId: undefined, approval: undefined, ask: undefined, seq: stats.seq, turnStartAt: 0, turnProcessStartAt: 0, turnTokens: 0, turnUsageTokens: 0 };
    }
    case "meta": return sameMeta(s.meta, a.meta) ? s : { ...s, meta: a.meta };
    case "context": {
      const sessionTokens = typeof a.context.sessionTokens === "number"
        ? Math.max(0, a.context.sessionTokens)
        : s.sessionTokens;
      const usage = usageFromContext(a.context, s.usage);
      const costAvailable = a.context.costAvailable === true;
      const sessionCost = costAvailable
        ? (typeof a.context.sessionCost === "number" ? a.context.sessionCost : typeof a.context.sessionCostUsd === "number" ? a.context.sessionCostUsd : 0)
        : 0;
      const sessionCurrency = costAvailable ? (a.context.sessionCurrency || usage?.currency || "") : "";
      return { ...s, context: { ...a.context, costAvailable }, usage, sessionTokens, sessionCost, sessionCurrency, costAvailable };
    }
    case "balance": return { ...s, balance: a.balance };
    case "effort": return { ...s, effort: a.effort };
    case "jobs": return { ...s, jobs: a.jobs };
    case "checkpoints": return { ...s, checkpoints: a.checkpoints };
    case "message_action_start": return { ...s, messageAction: a.action };
    case "message_action_done": return { ...s, messageAction: undefined };
    case "history": {
      const { items, seq } = historyMessagesToItems(a.messages, "h", s.seq);
      return { ...s, items, seq };
    }
    case "session_load_start": return { ...s, loading: true, hydrating: a.hydrating };
    case "session_primary_loaded": return { ...s, loading: false };
    case "session_hydrated": return s.hydrating ? { ...s, hydrating: false } : s;
    case "local_notice": return { ...s, running: false, turnActive: false, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `n${s.seq}`, level: a.level, text: a.text }] };
    case "runtime_switch": {
      const progress = a.progress;
      if (s.runtimeSwitch && progress.generation < s.runtimeSwitch.generation) return s;
      if (!progress.recorded) return { ...s, runtimeSwitch: progress };
      const index = s.items.findIndex((item) => item.kind === "mode_switch" && item.id === progress.switchId);
      const item: Item = {
        kind: "mode_switch",
        id: progress.switchId,
        fromMode: progress.fromMode,
        toMode: progress.toMode,
        appliedMode: progress.appliedMode,
        phase: progress.phase,
        progress: progress.progress,
        startedAt: progress.startedAt,
        completedAt: progress.completedAt,
        error: progress.error,
      };
      if (index < 0) {
        return { ...s, runtimeSwitch: progress, items: [...s.items, item], seq: s.seq + 1 };
      }
      const items = [...s.items];
      items[index] = item;
      return { ...s, runtimeSwitch: progress, items };
    }
    case "steer_sent": {
      const text = a.text.trim();
      if (!text) return s;
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "steer", id: `s${s.seq}`, text }] };
    }
    case "clearApproval": return { ...s, approval: undefined };
    case "clearAsk": return { ...s, ask: undefined };
    case "reset": return { ...initialState, meta: s.meta, context: { ...s.context, used: 0, sessionTokens: 0 }, balance: s.balance, effort: s.effort, jobs: s.jobs };
    case "event": return applyEvent(s, a.e);
    default: return s;
  }
}

// ---- per-tab state map ----

type TabStates = Map<string, State>;

const CONTEXT_REFRESH_INTERVAL_MS = 4000;

export function nextContextRefreshDelay(now: number, lastRefreshAt: number | undefined, intervalMs = CONTEXT_REFRESH_INTERVAL_MS): number {
  if (!lastRefreshAt || lastRefreshAt <= 0) return 0;
  return Math.max(0, intervalMs - Math.max(0, now - lastRefreshAt));
}

function getOrCreateState(states: TabStates, tabId: string): State {
  if (!states.has(tabId)) states.set(tabId, { ...initialState });
  return states.get(tabId)!;
}

function messageActionBusyText(scope: MessageActionScope): string {
  switch (scope) {
    case "fork":
      return t("rewind.busyFork");
    case "summ-from":
      return t("rewind.busySummFrom");
    case "summ-upto":
      return t("rewind.busySummUpto");
    case "conversation":
      return t("rewind.busyConversation");
    case "code":
      return t("rewind.busyCode");
    default:
      return t("rewind.busyBoth");
  }
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return String(err || "");
}

function afterNextPaint(): Promise<void> {
  return new Promise((resolve) => {
    let settled = false;
    const finish = () => {
      if (settled) return;
      settled = true;
      window.clearTimeout(fallback);
      resolve();
    };
    const fallback = window.setTimeout(finish, 50);
    window.requestAnimationFrame(finish);
  });
}

async function refreshMetaForTab(tabId: string, dispatchTo: (tabId: string, action: Action) => void): Promise<void> {
  try {
    dispatchTo(tabId, { type: "meta", meta: await app.MetaForTab(tabId) });
    dispatchTo(tabId, { type: "effort", effort: await app.EffortForTab(tabId) });
  } catch {
    /* ignore */
  }
}

export function useController() {
  const statesRef = useRef<TabStates>(new Map());
  const lastTokenAt = useRef(0);
  const [activeTabId, setActiveTabId] = useState<string | undefined>();
  const activeTabIdRef = useRef<string | undefined>(undefined);
  // A render-triggering counter so that mutations to a non-active tab's state still
  // cause a re-render when that tab becomes active.
  const [, setVersion] = useState(0);
  const bump = useCallback(() => setVersion((v) => v + 1), []);

  // The active tab's current state, with a stable identity for cancel().
  const activeState = activeTabId ? getOrCreateState(statesRef.current, activeTabId) : initialState;
  const stateRef = useRef(activeState);
  activeTabIdRef.current = activeTabId;
  stateRef.current = activeState;

  // Dispatch to a specific tab's state. If the tab doesn't have state yet, it's
  // created. Bumps the version so React re-renders when it becomes active.
  const dispatchTo = useCallback((tabId: string, action: Action) => {
    const states = statesRef.current;
    const prev = getOrCreateState(states, tabId);
    const next = reducer(prev, action);
    if (prev !== next) {
      states.set(tabId, next);
      bump();
    }
  }, [bump]);

  const checkpointRefreshSeq = useRef(new Map<string, number>());
  const sessionLoadSeq = useRef(new Map<string, number>());
  const contextRefreshSeq = useRef(new Map<string, number>());
  const contextRefreshTimers = useRef(new Map<string, number>());
  const lastContextRefreshAt = useRef(new Map<string, number>());
  const bumpSessionLoadSeq = useCallback((tabId: string): number => {
    const seq = (sessionLoadSeq.current.get(tabId) ?? 0) + 1;
    sessionLoadSeq.current.set(tabId, seq);
    return seq;
  }, []);
  const sessionLoadCurrent = useCallback((tabId: string, seq: number): boolean => {
    return sessionLoadSeq.current.get(tabId) === seq;
  }, []);
  const bumpCheckpointRefreshSeq = useCallback((tabId: string): number => {
    const seq = (checkpointRefreshSeq.current.get(tabId) ?? 0) + 1;
    checkpointRefreshSeq.current.set(tabId, seq);
    return seq;
  }, []);
  const refreshCheckpoints = useCallback(async (tabId: string) => {
    const seq = bumpCheckpointRefreshSeq(tabId);
    const checkpoints = await app.CheckpointsForTab(tabId).catch(() => undefined);
    if (checkpointRefreshSeq.current.get(tabId) !== seq || checkpoints === undefined) return;
    dispatchTo(tabId, { type: "checkpoints", checkpoints: asArray(checkpoints) });
  }, [bumpCheckpointRefreshSeq, dispatchTo]);

  const refreshContextForTab = useCallback(async (tabId: string) => {
    const seq = (contextRefreshSeq.current.get(tabId) ?? 0) + 1;
    contextRefreshSeq.current.set(tabId, seq);
    lastContextRefreshAt.current.set(tabId, Date.now());
    const context = await app.ContextUsageForTab(tabId).catch(() => undefined);
    if (contextRefreshSeq.current.get(tabId) !== seq || context === undefined) return;
    dispatchTo(tabId, { type: "context", context });
  }, [dispatchTo]);

  const refreshBalanceForTab = useCallback((tabId: string) => {
    // Clear the previous provider's value immediately. Unsupported providers
    // remain absent instead of flashing a misleading loading placeholder.
    dispatchTo(tabId, { type: "balance", balance: { available: false, display: "" } });
    app.BalanceForTab(tabId)
      .then((balance) => dispatchTo(tabId, { type: "balance", balance }))
      .catch((err) => dispatchTo(tabId, {
        type: "balance",
        balance: { available: false, display: "", err: err instanceof Error ? err.message : String(err) },
      }));
  }, [dispatchTo]);

  const requestContextRefresh = useCallback((tabId: string, minDelay = 0) => {
    const now = Date.now();
    const wait = Math.max(minDelay, nextContextRefreshDelay(now, lastContextRefreshAt.current.get(tabId)));
    const existing = contextRefreshTimers.current.get(tabId);
    if (wait <= 0) {
      if (existing !== undefined) {
        window.clearTimeout(existing);
        contextRefreshTimers.current.delete(tabId);
      }
      void refreshContextForTab(tabId);
      return;
    }
    if (existing !== undefined) return;
    const timer = window.setTimeout(() => {
      contextRefreshTimers.current.delete(tabId);
      void refreshContextForTab(tabId);
    }, wait);
    contextRefreshTimers.current.set(tabId, timer);
  }, [refreshContextForTab]);

  useEffect(() => () => {
    for (const timer of contextRefreshTimers.current.values()) {
      window.clearTimeout(timer);
    }
    contextRefreshTimers.current.clear();
  }, []);

  const loadSessionDataForTab = useCallback(async (tabId: string, reset = false) => {
    const seq = bumpSessionLoadSeq(tabId);
    const safe = <T,>(p: Promise<T>): Promise<T | undefined> => p.catch(() => undefined);
    if (reset) dispatchTo(tabId, { type: "reset" });
    const current = statesRef.current.get(tabId);
    dispatchTo(tabId, { type: "session_load_start", hydrating: reset || !current || current.items.length === 0 });

    // Meta and history are the only first-paint dependencies. Each is applied
    // as soon as it arrives so a slow auxiliary endpoint cannot delay the chat.
    const metaLoad = safe(app.MetaForTab(tabId)).then((meta) => {
      if (meta && sessionLoadCurrent(tabId, seq)) dispatchTo(tabId, { type: "meta", meta });
    });
    const historyLoad = safe(app.HistoryForTab(tabId)).then((history) => {
      if (history !== undefined && sessionLoadCurrent(tabId, seq)) {
        dispatchTo(tabId, { type: "history", messages: asArray(history) });
      }
    });
    await Promise.all([metaLoad, historyLoad]);
    if (!sessionLoadCurrent(tabId, seq)) return;
    dispatchTo(tabId, { type: "session_primary_loaded" });
    window.requestAnimationFrame(() => {
      if (sessionLoadCurrent(tabId, seq)) dispatchTo(tabId, { type: "session_hydrated" });
    });

    // Auxiliary status hydrates independently after the transcript is usable.
    void safe(app.EffortForTab(tabId)).then((effort) => {
      if (effort && sessionLoadCurrent(tabId, seq)) dispatchTo(tabId, { type: "effort", effort });
    });
    void safe(app.JobsForTab(tabId)).then((jobs) => {
      if (jobs && sessionLoadCurrent(tabId, seq)) dispatchTo(tabId, { type: "jobs", jobs: asArray(jobs) });
    });
    void safe(app.CheckpointsForTab(tabId)).then((checkpoints) => {
      if (checkpoints && sessionLoadCurrent(tabId, seq)) dispatchTo(tabId, { type: "checkpoints", checkpoints: asArray(checkpoints) });
    });
    refreshBalanceForTab(tabId);
    requestContextRefresh(tabId);
  }, [bumpSessionLoadSeq, dispatchTo, refreshBalanceForTab, requestContextRefresh, sessionLoadCurrent]);

  const activeTabFromBackend = useCallback(async (): Promise<TabMeta | undefined> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    return tabs.find((tab) => tab.active) ?? tabs[0];
  }, []);

  const waitForTabReady = useCallback(async (tabId: string): Promise<void> => {
    for (let attempt = 0; attempt < 60; attempt += 1) {
      const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
      const tab = tabs.find((candidate) => candidate.id === tabId);
      if (!tab || tab.ready || tab.startupErr) return;
      await new Promise((resolve) => window.setTimeout(resolve, 100));
    }
  }, []);

  const syncActiveTabFromBackend = useCallback(async (reset = false): Promise<string | undefined> => {
    const active = await activeTabFromBackend();
    if (!active) return undefined;
    setActiveTabId(active.id);
    await loadSessionDataForTab(active.id, reset);
    return active.id;
  }, [activeTabFromBackend, loadSessionDataForTab]);

  const reconcileTabRuntime = useCallback(async (tabId: string) => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    const tab = tabs.find((candidate) => candidate.id === tabId);
    if (!tab) return;
    const local = statesRef.current.get(tabId);
    const needsInitialLoad = !local?.meta;
    const missedTurnDone = Boolean(local?.running && !tab.running);
    dispatchTo(tabId, { type: "backend_status", running: Boolean(tab.running) });
    if (needsInitialLoad || missedTurnDone) {
      await loadSessionDataForTab(tabId, missedTurnDone);
      return;
    }
    const [jobs, effort] = await Promise.all([
      app.JobsForTab(tabId).catch(() => undefined),
      app.EffortForTab(tabId).catch(() => undefined),
    ]);
    void refreshContextForTab(tabId);
    if (jobs) dispatchTo(tabId, { type: "jobs", jobs: asArray(jobs) });
    if (effort) dispatchTo(tabId, { type: "effort", effort });
    refreshBalanceForTab(tabId);
  }, [dispatchTo, loadSessionDataForTab, refreshBalanceForTab, refreshContextForTab]);

  useEffect(() => {
    const textBatch = createRafBatch<{ tabId: string; e: WireEvent }>((batch) => {
      for (const { tabId, e } of batch) dispatchTo(tabId, { type: "event", e });
    });
    const off = onEvent((e) => {
      const targetTabId = e.tabId || activeTabIdRef.current;
      if (!targetTabId) return;
      if (e.kind === "session_updated") {
        textBatch.drain();
        void loadSessionDataForTab(targetTabId, true);
        void refreshMetaForTab(targetTabId, dispatchTo);
        void refreshCheckpoints(targetTabId);
        requestContextRefresh(targetTabId);
        return;
      }
      if (e.kind === "turn_started" || e.kind === "text" || e.kind === "reasoning") {
        lastTokenAt.current = Date.now();
      }
      if (e.kind === "text" || e.kind === "reasoning") {
        textBatch.push({ tabId: targetTabId, e });
      } else {
        textBatch.drain();
        dispatchTo(targetTabId, { type: "event", e });
      }
      if (e.kind === "turn_started" || e.kind === "usage" || e.kind === "message" || e.kind === "tool_result" || e.kind === "turn_done" || e.kind === "compaction_done") {
        requestContextRefresh(targetTabId, e.kind === "turn_started" ? 150 : 0);
      }
      if (e.kind === "turn_done") {
        void refreshMetaForTab(targetTabId, dispatchTo);
        requestContextRefresh(targetTabId);
        refreshBalanceForTab(targetTabId);
        app.EffortForTab(targetTabId).then((effort) => dispatchTo(targetTabId, { type: "effort", effort })).catch(() => {});
        void refreshCheckpoints(targetTabId);
      }
      if (e.kind === "compaction_done") {
        requestContextRefresh(targetTabId);
      }
      if (e.kind === "turn_done" || e.kind === "notice") {
        app.JobsForTab(targetTabId).then((jobs) => dispatchTo(targetTabId, { type: "jobs", jobs: asArray(jobs) })).catch(() => {});
      }
    });

    const offRuntimeSwitch = onRuntimeSwitchProgress((progress) => {
      if (!progress.tabId || !progress.switchId) return;
      dispatchTo(progress.tabId, { type: "runtime_switch", progress });
    });

    const offReady = onReady(() => {
      const readyTabId = activeTabIdRef.current;
      if (readyTabId) {
        void loadSessionDataForTab(readyTabId);
        return;
      }
      void syncActiveTabFromBackend();
    });

    void syncActiveTabFromBackend();
    // The event subscription is live now, so ask the backend to re-emit any
    // approval/ask prompt that was already blocking a tab before this load —
    // otherwise a session left mid-confirmation shows "waiting" with no modal
    // and no way to stop (#3844).
    void app.ReplayPendingPrompts().catch(() => {});

    return () => { textBatch.drain(); off(); offRuntimeSwitch(); offReady(); };
  }, [dispatchTo, loadSessionDataForTab, refreshBalanceForTab, refreshCheckpoints, requestContextRefresh, syncActiveTabFromBackend]);

  useEffect(() => {
    if (!activeTabId) return;
    requestContextRefresh(activeTabId);
  }, [activeTabId, requestContextRefresh]);

  useEffect(() => {
    if (!activeTabId || !activeState.running) return;
    requestContextRefresh(activeTabId, 150);
    const timer = window.setInterval(() => {
      requestContextRefresh(activeTabId);
    }, CONTEXT_REFRESH_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [activeTabId, activeState.running, requestContextRefresh]);

  // Stale-stream watchdog: if the frontend thinks the agent is running but
  // no token events have arrived for 30 seconds, reconcile with the backend.
  // This catches the case where the Wails event channel silently drops the
  // turn_done event after a model-service interruption (#3746).
  useEffect(() => {
    if (!activeTabId) return;
    const s = statesRef.current.get(activeTabId);
    if (!s?.running || !s.live) return;
    const since = Date.now() - lastTokenAt.current;
    if (since >= 30_000) {
      void reconcileTabRuntime(activeTabId);
      return;
    }
    const timer = window.setTimeout(() => {
      const cur = statesRef.current.get(activeTabId);
      if (cur?.running && cur.live && Date.now() - lastTokenAt.current >= 30_000) {
        void reconcileTabRuntime(activeTabId);
      }
    }, 30_000 - since);
    return () => window.clearTimeout(timer);
  }, [activeTabId, reconcileTabRuntime, activeState.running, activeState.live]);

  const send = useCallback(async (displayText: string, submitText = displayText): Promise<void> => {
    const submitForTab = async (tabId: string): Promise<void> => {
      const seq = getOrCreateState(statesRef.current, tabId).seq;
      dispatchTo(tabId, { type: "user", text: displayText, seq });
      const display = displayText.trim();
      const submit = submitText.trim();
      try {
        await (display !== submit ? app.SubmitDisplayToTab(tabId, display, submit) : app.SubmitToTab(tabId, submit));
      } catch (error) {
        dispatchTo(tabId, { type: "send_failed", error: `Send failed: ${error instanceof Error ? error.message : String(error)}` });
        throw error;
      }
    };
    const tabId = activeTabIdRef.current ?? activeTabId;
    if (tabId) {
      await submitForTab(tabId);
      return;
    }
    const active = await activeTabFromBackend();
    if (!active?.id) throw new Error("Cannot send: no active tab is available.");
    setActiveTabId(active.id);
    await submitForTab(active.id);
  }, [activeTabFromBackend, activeTabId, dispatchTo]);

  const runShell = useCallback((command: string) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "user", text: `!${command}`, seq: getOrCreateState(statesRef.current, activeTabId).seq });
    app.RunShellForTab(activeTabId, command).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const steer = useCallback((text: string) => {
    if (!activeTabId) return;
    const trimmed = text.trim();
    if (!trimmed) return;
    // A steer is not a backend turn, so keep it out of user bubbles/turn
    // counting. Still render it immediately so mid-turn guidance never feels lost.
    dispatchTo(activeTabId, { type: "steer_sent", text: trimmed });
    app.SteerForTab(activeTabId, trimmed).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const notice = useCallback((text: string, level: "info" | "warn" = "info") => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "local_notice", level, text });
  }, [activeTabId, dispatchTo]);

  const cancel = useCallback((): string | undefined => {
    const cur = stateRef.current;
    const tabId = activeTabId;
    if (cur.running && cur.pendingUser !== undefined) {
      const text = cur.pendingUser;
      if (tabId) {
        dispatchTo(tabId, { type: "unsend" });
        dispatchTo(tabId, { type: "cancel_requested" });
        app.RequestCancelTab(tabId).then((ack) => {
          if (!ack.accepted) dispatchTo(tabId, { type: "cancel_rejected" });
        }).catch(() => dispatchTo(tabId, { type: "cancel_rejected" }));
      }
      return text;
    }
    if (tabId) {
      dispatchTo(tabId, { type: "cancel_requested" });
      app.RequestCancelTab(tabId).then((ack) => {
        if (!ack.accepted) dispatchTo(tabId, { type: "cancel_rejected" });
      }).catch(() => dispatchTo(tabId, { type: "cancel_rejected" }));
    }
    return undefined;
  }, [activeTabId, dispatchTo]);

  const approve = useCallback((id: string, allow: boolean, session: boolean, persist: boolean) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "clearApproval" });
    app.ApproveTab(activeTabId, id, allow, session, persist).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const answerQuestion = useCallback((id: string, answers: QuestionAnswer[]) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "clearAsk" });
    app.AnswerQuestionForTab(activeTabId, id, answers).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const setControllerMode = useCallback((mode: Mode): Promise<void> => {
    if (!activeTabId) return Promise.resolve();
    return app.SetModeForTab(activeTabId, mode).then(() => {
      if (modeHasAutoApproveTools(mode) && activeTabId) dispatchTo(activeTabId, { type: "clearApproval" });
    }).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const setCollaborationMode = useCallback(async (mode: CollaborationMode): Promise<void> => {
    if (!activeTabId) return;
    await app.SetCollaborationModeForTab(activeTabId, mode).catch(() => {});
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const setToolApprovalMode = useCallback(async (mode: ToolApprovalMode): Promise<void> => {
    if (!activeTabId) return;
    await app.SetToolApprovalModeForTab(activeTabId, mode).catch(() => {});
    if (mode === "auto" || mode === "yolo") dispatchTo(activeTabId, { type: "clearApproval" });
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const setGoal = useCallback(async (goal: string): Promise<void> => {
    if (!activeTabId) return;
    await app.SetGoalForTab(activeTabId, goal).catch(() => {});
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const clearGoal = useCallback(async (): Promise<void> => {
    if (!activeTabId) return;
    await app.ClearGoalForTab(activeTabId).catch(() => {});
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const setAskWorkflow = useCallback(async (enabled: boolean): Promise<void> => {
    if (!activeTabId) return;
    await app.SetAskWorkflowForTab(activeTabId, enabled).catch(() => {});
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const setStepThinking = useCallback(async (enabled: boolean): Promise<void> => {
    if (!activeTabId) return;
    await app.SetStepThinkingForTab(activeTabId, enabled).catch(() => {});
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const setEnhancedMode = useCallback(async (enabled: boolean): Promise<void> => {
    if (!activeTabId) return;
    try {
      await app.SetEnhancedModeForTab(activeTabId, enabled);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: errorMessage(err) });
      return;
    }
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.MetaForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "effort", effort: await app.EffortForTab(activeTabId) });
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo]);

  const togglePause = useCallback(async (): Promise<void> => {
    if (!activeTabId) return;
    const paused = Boolean(getOrCreateState(statesRef.current, activeTabId).meta?.paused);
    try {
      if (paused) await app.ResumeTab(activeTabId);
      else await app.PauseTab(activeTabId);
      await refreshMetaForTab(activeTabId, dispatchTo);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: errorMessage(err) });
    }
  }, [activeTabId, dispatchTo]);

  const newSession = useCallback(async () => {
    const tabId = activeTabId;
    if (tabId) bumpCheckpointRefreshSeq(tabId);
    try {
      await app.NewSession();
    } catch {
      return; // backend refused (workspace starting / failed) — keep the transcript
    }
    if (tabId) dispatchTo(tabId, { type: "reset" });
    if (tabId) await refreshMetaForTab(tabId, dispatchTo);
  }, [activeTabId, bumpCheckpointRefreshSeq, dispatchTo]);

  const clearSession = useCallback(async () => {
    const tabId = activeTabId;
    if (tabId) bumpCheckpointRefreshSeq(tabId);
    try {
      await app.ClearSession();
    } catch {
      return;
    }
    if (tabId) dispatchTo(tabId, { type: "reset" });
  }, [activeTabId, bumpCheckpointRefreshSeq, dispatchTo]);

  const listSessions = useCallback(async (): Promise<SessionMeta[]> => asArray<SessionMeta>(await app.ListSessions().catch(() => [])), []);
  const listTrashedSessions = useCallback(async (): Promise<SessionMeta[]> => asArray<SessionMeta>(await app.ListTrashedSessions().catch(() => [])), []);
  const resumeSession = useCallback(async (path: string, tabId?: string) => {
    const targetTabId = tabId || activeTabId;
    if (!targetTabId) return;
    if (tabId) await waitForTabReady(tabId);
    const messages = asArray(
      await (tabId ? app.ResumeSessionForTab(tabId, path) : app.ResumeSession(path)).catch(() => [] as HistoryMessage[]),
    );
    if (messages.length === 0) return;
    dispatchTo(targetTabId, { type: "reset" });
    dispatchTo(targetTabId, { type: "history", messages });
    void refreshContextForTab(targetTabId);
    void refreshCheckpoints(targetTabId);
  }, [activeTabId, dispatchTo, refreshCheckpoints, refreshContextForTab, waitForTabReady]);

  const previewSession = useCallback(async (path: string): Promise<HistoryMessage[]> => asArray<HistoryMessage>(await app.PreviewSession(path).catch(() => [])), []);
  const deleteSession = useCallback((path: string) => app.DeleteSession(path).catch(() => {}), []);
  const restoreSession = useCallback((path: string) => app.RestoreSession(path).catch(() => {}), []);
  const purgeTrashedSession = useCallback((path: string) => app.PurgeTrashedSession(path).catch(() => {}), []);
  const renameSession = useCallback((path: string, title: string) => app.RenameSession(path, title).catch(() => {}), []);

  const refreshMeta = useCallback(async () => {
    if (!activeTabId) return;
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.MetaForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "effort", effort: await app.EffortForTab(activeTabId) });
      await refreshContextForTab(activeTabId);
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo, refreshContextForTab]);

  const refreshWorkspaceState = useCallback(async (path: string): Promise<string> => {
    if (path) await syncActiveTabFromBackend(true);
    return path;
  }, [syncActiveTabFromBackend]);

  const pickWorkspace = useCallback(async (): Promise<string> => {
    const path = await app.PickWorkspace().catch(() => "");
    return refreshWorkspaceState(path);
  }, [refreshWorkspaceState]);
  const switchWorkspace = useCallback(async (path: string): Promise<string> => {
    const next = await app.SwitchWorkspace(path).catch(() => "");
    return refreshWorkspaceState(next);
  }, [refreshWorkspaceState]);

  const compact = useCallback(() => { app.Compact().catch(() => {}); }, []);

  const setModel = useCallback(async (name: string) => {
    if (!activeTabId) return;
    try {
      await app.SetModelForTab(activeTabId, name);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: t("status.modelSwitchFailed", { err: errorMessage(err) }) });
      return;
    }
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.MetaForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "effort", effort: await app.EffortForTab(activeTabId) });
      await refreshContextForTab(activeTabId);
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo, refreshContextForTab]);

  const setEffort = useCallback(async (level: string) => {
    if (!activeTabId) return;
    await app.SetEffortForTab(activeTabId, level).catch(() => {});
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.MetaForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "effort", effort: await app.EffortForTab(activeTabId) });
      await refreshContextForTab(activeTabId);
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo, refreshContextForTab]);

  const fetchMemory = useCallback((): Promise<MemoryView> =>
    app.Memory().catch(() => ({ docs: [], facts: [], scopes: [], storeDir: "", available: false })), []);
  const remember = useCallback(async (scope: string, note: string) => { await app.Remember(scope, note).catch(() => {}); }, []);
  const forget = useCallback(async (name: string) => { await app.Forget(name).catch(() => {}); }, []);
  const saveDoc = useCallback(async (path: string, body: string) => { await app.SaveDoc(path, body).catch(() => {}); }, []);

  const rewind = useCallback(async (turn: number, scope: string) => {
    const sourceTabId = activeTabId;
    if (!sourceTabId) return;
    const actionScope = (["fork", "summ-from", "summ-upto", "conversation", "code", "both"].includes(scope) ? scope : "both") as MessageActionScope;
    dispatchTo(sourceTabId, { type: "message_action_start", action: { turn, scope: actionScope } });
    dispatchTo(sourceTabId, { type: "local_notice", level: "info", text: messageActionBusyText(actionScope) });
    try {
      if (actionScope === "fork") {
        const tab = await app.Fork(turn);
        if (tab?.id) {
          setActiveTabId(tab.id);
          // The fork's controller builds in a background goroutine: an immediate
          // load reads empty history, and the ready-event fallback can still
          // target the source tab, leaving the fork blank (#3742).
          await waitForTabReady(tab.id);
          await loadSessionDataForTab(tab.id, true);
        } else {
          await syncActiveTabFromBackend(true);
        }
        return;
      }

      if (actionScope === "summ-from") await app.SummarizeFrom(turn);
      else if (actionScope === "summ-upto") await app.SummarizeUpTo(turn);
      else await app.Rewind(turn, actionScope);

      const messages = asArray(await app.HistoryForTab(sourceTabId).catch(() => [] as HistoryMessage[]));
      dispatchTo(sourceTabId, { type: "reset" });
      if (messages.length) dispatchTo(sourceTabId, { type: "history", messages });
      dispatchTo(sourceTabId, { type: "meta", meta: await app.MetaForTab(sourceTabId) });
      await refreshContextForTab(sourceTabId);
      dispatchTo(sourceTabId, { type: "checkpoints", checkpoints: asArray(await app.CheckpointsForTab(sourceTabId)) });
    } catch {
      /* The controller emits a warning notice with the specific failure reason. */
    } finally {
      dispatchTo(sourceTabId, { type: "message_action_done" });
    }
  }, [activeTabId, dispatchTo, loadSessionDataForTab, refreshContextForTab, syncActiveTabFromBackend, waitForTabReady]);

  // Tab management: switch preserves per-tab state; open creates it.
  const switchTab = useCallback(async (tabId: string) => {
    setActiveTabId(tabId);
    try {
      await app.SetActiveTab(tabId);
      await reconcileTabRuntime(tabId);
    } catch { /* ignore */ }
  }, [reconcileTabRuntime]);

  const openProjectTab = useCallback(async (workspaceRoot: string, topicId: string): Promise<TabMeta> => {
    const meta = await app.OpenProjectTab(workspaceRoot, topicId);
    const reset = !statesRef.current.has(meta.id);
    dispatchTo(meta.id, { type: "session_load_start", hydrating: reset });
    setActiveTabId(meta.id);
    await afterNextPaint();
    await loadSessionDataForTab(meta.id, reset);
    return meta;
  }, [dispatchTo, loadSessionDataForTab]);

  const openGlobalTab = useCallback(async (topicId: string): Promise<TabMeta> => {
    const meta = await app.OpenGlobalTab(topicId);
    const reset = !statesRef.current.has(meta.id);
    dispatchTo(meta.id, { type: "session_load_start", hydrating: reset });
    setActiveTabId(meta.id);
    await afterNextPaint();
    await loadSessionDataForTab(meta.id, reset);
    return meta;
  }, [dispatchTo, loadSessionDataForTab]);

  const openAutomationTab = useCallback(async (topicId: string): Promise<TabMeta> => {
    const meta = await app.OpenAutomationTab(topicId);
    const reset = !statesRef.current.has(meta.id);
    dispatchTo(meta.id, { type: "session_load_start", hydrating: reset });
    setActiveTabId(meta.id);
    await afterNextPaint();
    await loadSessionDataForTab(meta.id, reset);
    return meta;
  }, [dispatchTo, loadSessionDataForTab]);

  // Ensure a blank tab exists for the given scope — reuses an existing one
  // or creates a new tab, then loads its session data.
  const ensureBlankTab = useCallback(async (scope: string, workspaceRoot: string): Promise<TabMeta> => {
    const meta = await app.EnsureBlankTab(scope, workspaceRoot);
    const reset = !statesRef.current.has(meta.id);
    dispatchTo(meta.id, { type: "session_load_start", hydrating: reset });
    setActiveTabId(meta.id);
    await afterNextPaint();
    await loadSessionDataForTab(meta.id, reset);
    return meta;
  }, [dispatchTo, loadSessionDataForTab]);

  const closeTab = useCallback(async (tabId: string) => {
    try {
      await app.CloseTab(tabId);
      statesRef.current.delete(tabId);
      bump();
      if (tabId === activeTabId) await syncActiveTabFromBackend(true);
    } catch { /* ignore */ }
  }, [activeTabId, bump, syncActiveTabFromBackend]);

  const reorderTabs = useCallback(async (tabIds: string[]) => {
    try {
      await app.ReorderTabs(tabIds);
    } catch { /* ignore */ }
  }, []);

  return {
    state: activeState,
    runtimeSwitch: activeState.runtimeSwitch,
    activeTabId,
    send, runShell, steer, notice, cancel, approve, answerQuestion, setControllerMode, setCollaborationMode, setToolApprovalMode, setAskWorkflow, setStepThinking, setEnhancedMode, togglePause, setGoal, clearGoal,
    newSession, clearSession, listSessions, listTrashedSessions, resumeSession, previewSession, deleteSession, restoreSession, purgeTrashedSession, renameSession,
    refreshMeta, pickWorkspace, switchWorkspace, compact, rewind, setModel, setEffort,
    fetchMemory, remember, forget, saveDoc,
    switchTab, openProjectTab, openGlobalTab, openAutomationTab, ensureBlankTab, closeTab, reorderTabs,
    syncActiveTab: syncActiveTabFromBackend,
  };
}
