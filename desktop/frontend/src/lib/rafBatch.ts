// Coalesces text/reasoning stream deltas into one flush per animation frame.
// Non-text events must drain() first so causal ordering is preserved.

type Flush<T> = (batch: T[]) => void;

interface BatchHandle<T> {
  push: (item: T) => void;
  drain: () => void;
  size: () => number;
}

export function createRafBatch<T>(flush: Flush<T>): BatchHandle<T> {
  let buffer: T[] = [];
  let scheduled: number | null = null;
  let deadline: ReturnType<typeof setTimeout> | null = null;

  const unschedule = () => {
    if (scheduled !== null && typeof cancelAnimationFrame !== "undefined") cancelAnimationFrame(scheduled);
    if (deadline !== null) clearTimeout(deadline);
    scheduled = null;
    deadline = null;
  };

  const run = () => {
    unschedule();
    // Snapshot + clear before flushing so a re-entrant push() lands next frame.
    const out = buffer;
    buffer = [];
    if (out.length > 0) flush(out);
  };

  const handle: BatchHandle<T> = {
    push(item: T) {
      buffer.push(item);
      if (deadline === null) {
        // WebViews may delay rAF while occluded. Do not retain deltas until a
        // final Message event just because no animation frame was delivered.
        deadline = setTimeout(run, 50);
        if (typeof requestAnimationFrame !== "undefined") scheduled = requestAnimationFrame(run);
      }
    },
    drain() {
      run();
    },
    size() {
      return buffer.length;
    },
  };
  return handle;
}
