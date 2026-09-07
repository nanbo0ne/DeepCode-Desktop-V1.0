import type { ContextInfo, ContextPanelInfo, WireUsage } from "./types";

export interface ContextPanelUsageSummary {
  usedTokens: number;
  windowTokens: number;
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  reasoningTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  currentPromptTokens: number;
  currentCompletionTokens: number;
  currentReasoningTokens: number;
  currentCacheHitTokens: number;
  currentCacheMissTokens: number;
}

interface ContextPanelUsageInput {
  context?: ContextInfo;
  info?: ContextPanelInfo | null;
  usage?: WireUsage;
  sessionTokens?: number;
}

export function resolveContextPanelRequestCount(
  info?: ContextPanelInfo | null,
  context?: ContextInfo,
): number {
  if (typeof info?.requestCount === "number" && info.requestCount >= 0) return info.requestCount;
  if (typeof context?.requestCount === "number" && context.requestCount >= 0) return context.requestCount;
  return 0;
}

export function contextPanelCurrencySymbol(currency?: string): string | null {
  const value = (currency || "").trim();
  if (!value) return null;
  if (/^(cny|rmb|yuan|¥|￥)$/i.test(value)) return "¥";
  if (/^(usd|dollar|\$)$/i.test(value)) return "$";
  if (/^(eur|euro|€)$/i.test(value)) return "€";
  if (/^(gbp|pound|£)$/i.test(value)) return "£";
  return null;
}

export function formatContextPanelMoney(amount: number, currency?: string): string {
  if (!Number.isFinite(amount) || amount <= 0) return "-";
  const formatted = amount < 1 ? amount.toFixed(4) : amount.toFixed(2);
  const value = (currency || "").trim();
  const symbol = contextPanelCurrencySymbol(value);
  if (symbol) return `${symbol}${formatted}`;
  return value ? `${value} ${formatted}` : formatted;
}

function positive(value?: number): number {
  return typeof value === "number" && value > 0 ? value : 0;
}

function inputTokensFromUsage(usage?: WireUsage): number {
  const promptTokens = positive(usage?.promptTokens);
  if (promptTokens > 0) return promptTokens;
  return positive(usage?.cacheHitTokens) + positive(usage?.cacheMissTokens);
}

export function computeContextPanelUsage({
  context,
  info,
  usage,
  sessionTokens,
}: ContextPanelUsageInput): ContextPanelUsageSummary {
  const sessionPromptTokens = positive(info?.sessionPromptTokens);
  const sessionCompletionTokens = positive(info?.sessionCompletionTokens);
  const sessionReasoningTokens = positive(info?.sessionReasoningTokens);
  const sessionCacheHitTokens = positive(info?.sessionCacheHitTokens);
  const sessionCacheMissTokens = positive(info?.sessionCacheMissTokens);
  const hasSessionBreakdown = Boolean(
    sessionPromptTokens > 0 ||
    sessionCompletionTokens > 0 ||
    sessionReasoningTokens > 0 ||
    sessionCacheHitTokens > 0 ||
    sessionCacheMissTokens > 0
  );
  const hasPanelBreakdown = Boolean(
    positive(info?.promptTokens) > 0 ||
    positive(info?.completionTokens) > 0 ||
    positive(info?.reasoningTokens) > 0 ||
    positive(info?.cacheHitTokens) > 0 ||
    positive(info?.cacheMissTokens) > 0
  );

  const currentPromptTokens = hasPanelBreakdown ? positive(info?.promptTokens) : inputTokensFromUsage(usage);
  const currentCompletionTokens = hasPanelBreakdown ? positive(info?.completionTokens) : positive(usage?.completionTokens);
  const currentReasoningTokens = hasPanelBreakdown ? positive(info?.reasoningTokens) : positive(usage?.reasoningTokens);
  const currentCacheHitTokens = hasPanelBreakdown ? positive(info?.cacheHitTokens) : positive(usage?.cacheHitTokens);
  const currentCacheMissTokens = hasPanelBreakdown ? positive(info?.cacheMissTokens) : positive(usage?.cacheMissTokens);

  const promptTokens = hasSessionBreakdown ? sessionPromptTokens : currentPromptTokens;
  const completionTokens = hasSessionBreakdown ? sessionCompletionTokens : currentCompletionTokens;
  const reasoningTokens = hasSessionBreakdown ? sessionReasoningTokens : currentReasoningTokens;
  const cacheHitTokens = hasSessionBreakdown ? sessionCacheHitTokens : currentCacheHitTokens;
  const cacheMissTokens = hasSessionBreakdown ? sessionCacheMissTokens : currentCacheMissTokens;

  const totalTokens =
    positive(info?.totalTokens) ||
    positive(sessionTokens) ||
    positive(usage?.totalTokens) ||
    promptTokens + completionTokens;

  const contextWindow = positive(context?.window);
  const panelWindow = positive(info?.windowTokens);
  const windowTokens = contextWindow || panelWindow;
  // A zero from either backend snapshot is authoritative after a controller
  // rebuild. Never substitute cumulative session usage for current occupancy.
  const snapshotUse = positive(context?.used) || positive(info?.usedTokens);

  return {
    usedTokens: snapshotUse,
    windowTokens,
    promptTokens,
    completionTokens,
    totalTokens,
    reasoningTokens,
    cacheHitTokens,
    cacheMissTokens,
    currentPromptTokens,
    currentCompletionTokens,
    currentReasoningTokens,
    currentCacheHitTokens,
    currentCacheMissTokens,
  };
}
