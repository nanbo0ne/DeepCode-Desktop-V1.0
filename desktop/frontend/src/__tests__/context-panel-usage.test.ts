// Run: tsx src/__tests__/context-panel-usage.test.ts

import {
  computeContextPanelUsage,
  contextPanelCurrencySymbol,
  formatContextPanelMoney,
  resolveContextPanelRequestCount,
} from "../lib/contextPanelUsage";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\ncontext panel usage");

eq(
  computeContextPanelUsage({
    context: { used: 0, window: 1000000, sessionTokens: 19337 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 19337,
      reasoningTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      requestCount: 2,
      readFiles: [],
      changedFiles: [],
    },
    usage: {
      promptTokens: 12000,
      completionTokens: 7000,
      totalTokens: 19000,
      reasoningTokens: 300,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      sessionCacheHitTokens: 0,
      sessionCacheMissTokens: 0,
    },
    sessionTokens: 19337,
  }).promptTokens,
  12000,
  "falls back to latest usage breakdown when panel only has totals",
);

eq(
  computeContextPanelUsage({
    context: { used: 0, window: 1000000, sessionTokens: 28393607 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 28393607,
      reasoningTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      sessionPromptTokens: 28330306,
      sessionCompletionTokens: 63301,
      sessionReasoningTokens: 18705,
      requestCount: 247,
      readFiles: [],
      changedFiles: [],
    },
    usage: {
      promptTokens: 900000,
      completionTokens: 50000,
      totalTokens: 950000,
      cacheHitTokens: 0,
      cacheMissTokens: 900000,
      sessionCacheHitTokens: 0,
      sessionCacheMissTokens: 28330306,
    },
    sessionTokens: 28393607,
  }).usedTokens,
  0,
  "keeps an authoritative zero snapshot after controller rebuild",
);

eq(
  computeContextPanelUsage({
    context: { used: 0, window: 1000000, sessionTokens: 19337 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 19337,
      reasoningTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      requestCount: 2,
      readFiles: [],
      changedFiles: [],
    },
    sessionTokens: 19337,
  }).usedTokens,
  0,
  "does not treat cumulative session totals as current context usage",
);

eq(
  computeContextPanelUsage({
    context: { used: 0, window: 1000000, sessionTokens: 19337 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 19337,
      reasoningTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      sessionPromptTokens: 12000,
      sessionCompletionTokens: 7000,
      sessionReasoningTokens: 300,
      requestCount: 2,
      readFiles: [],
      changedFiles: [],
    },
    sessionTokens: 19337,
  }).reasoningTokens,
  300,
  "uses persisted session breakdown after restart",
);

eq(
  computeContextPanelUsage({
    context: { used: 5000, window: 1000000, sessionTokens: 19337 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 3200,
      completionTokens: 900,
      totalTokens: 19337,
      reasoningTokens: 100,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      readFiles: [],
      changedFiles: [],
    },
  }).usedTokens,
  5000,
  "keeps the real context snapshot when it is available",
);

{
  const got = computeContextPanelUsage({
    context: { used: 6000, window: 100000, sessionTokens: 50000 },
    info: {
      usedTokens: 6000,
      windowTokens: 100000,
      promptTokens: 5900,
      completionTokens: 80,
      totalTokens: 50000,
      reasoningTokens: 20,
      cacheHitTokens: 3000,
      cacheMissTokens: 2900,
      sessionPromptTokens: 42000,
      sessionCompletionTokens: 7500,
      sessionReasoningTokens: 500,
      requestCount: 10,
      readFiles: [],
      changedFiles: [],
    },
  });
  eq(got.promptTokens, 42000, "keeps persisted cumulative prompt tokens for metrics");
  eq(got.currentPromptTokens, 5900, "keeps current prompt tokens for the context-window bar after compaction");
  eq(got.usedTokens, 6000, "uses recalculated compacted context usage after compaction");
}

eq(
  resolveContextPanelRequestCount({
    usedTokens: 0,
    windowTokens: 1000,
    promptTokens: 0,
    completionTokens: 0,
    totalTokens: 0,
    reasoningTokens: 0,
    cacheHitTokens: 0,
    cacheMissTokens: 0,
    requestCount: undefined,
    readFiles: [{ path: "a.ts", turn: 1, time: 1 }],
    changedFiles: [{ path: "b.ts", turns: [1], sources: [] }],
  }, undefined),
  0,
  "does not derive request count from referenced files",
);

eq(
  resolveContextPanelRequestCount(undefined, { used: 0, window: 1000, sessionTokens: 0, requestCount: 7 }),
  7,
  "falls back to the context snapshot request count",
);

eq(contextPanelCurrencySymbol("USD"), "$", "formats known currency codes with their symbols");
eq(formatContextPanelMoney(1.25, "CAD"), "CAD 1.25", "does not relabel an unsupported currency as yuan");
eq(formatContextPanelMoney(1.25), "1.25", "does not invent a currency when none is supplied");
eq(formatContextPanelMoney(Number.NaN, "USD"), "-", "does not render a non-finite cost");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
