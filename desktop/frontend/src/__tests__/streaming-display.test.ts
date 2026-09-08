import assert from "node:assert/strict";
import { createRafBatch } from "../lib/rafBatch";
import { initialState, reducer, type Item } from "../lib/useController";
import { buildTimelineSegments } from "../lib/transcriptTimeline";

const frames = new Map<number, FrameRequestCallback>();
let nextFrame = 0;
globalThis.requestAnimationFrame = (fn) => { frames.set(++nextFrame, fn); return nextFrame; };
globalThis.cancelAnimationFrame = (id) => { frames.delete(id); };
const batches: string[][] = [];
const batch = createRafBatch<string>((values) => batches.push(values));
batch.push("first");
batch.push("second");
await new Promise((resolve) => setTimeout(resolve, 75));
assert.deepEqual(batches, [["first", "second"]]);
assert.equal(frames.size, 0, "deadline cancels even animation frame ID 1");
batch.push("before tool");
batch.drain();
assert.deepEqual(batches[1], ["before tool"]);
await new Promise((resolve) => setTimeout(resolve, 65));
assert.equal(batches.length, 2, "a stale callback cannot duplicate flushed data");

let state = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "t1" } });
state = { ...state, items: [{ kind: "user", id: "u1", text: "test", turnId: "t1" }] };
for (const [kind, text] of [["reasoning", "private fixture reasoning"], ["text", "First partial "] , ["text", "reply"]] as const) {
  state = reducer(state, { type: "event", e: { kind, turnId: "t1", itemId: "a1", messageId: "m1", text } });
}
assert.equal(state.items.find((item) => item.kind === "assistant")?.text, "");
assert.equal(buildTimelineSegments(state.items, true)[1].kind, "assistant");
assert.equal(state.live?.text, "First partial reply");
assert.equal(state.live?.reasoning, "private fixture reasoning");
const finished: Item[] = [
  { kind: "user", id: "u", turnId: "done", text: "old" },
  { kind: "tool", id: "tool", turnId: "done", name: "read_file", args: "{}", readOnly: true, status: "done" },
  { kind: "assistant", id: "final", turnId: "done", text: "result", reasoning: "", streaming: false, final: true },
  { kind: "turn_stats", id: "stats", turnId: "done", success: true, outcome: "success" },
  ...state.items,
];
assert.equal(buildTimelineSegments(finished, true)[1].kind, "completed", "next turn cannot expand the previous turn");
const history = { ...initialState, items: finished.slice(0, 4) };
const newStream = reducer(history, { type: "event", e: { kind: "text", turnId: "next", messageId: "new-message", text: "new turn delta" } });
assert.equal(newStream.live?.id, "new-message", "missing item IDs must not match old messages");
const previousFinal = newStream.items.find(item => item.id === "final");
assert(previousFinal?.kind === "assistant");
assert.equal(previousFinal.text, "result");
console.log("PASS streaming deadlines, pre-commit consumer, completed-turn isolation");
