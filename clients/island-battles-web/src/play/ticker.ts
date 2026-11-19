// Timers that keep time in a hidden tab. Browsers slow a hidden tab's own timers (to once a second,
// and Chrome to once a minute after about five minutes hidden), but not a dedicated Worker's, nor
// the messages it posts. So the timers run in a small Worker that posts each one back when it is
// due. Its script is inline (a Blob URL), so neither the build nor the dev server bundles it.

/** What the schedule needs of a Worker; tests pass a fake. */
export interface WorkerLike {
  postMessage(message: unknown): void;
  addEventListener(
    type: "message" | "error",
    listener: (event: { data?: unknown }) => void,
  ): void;
}

/** Runs `run` after `ms`; returns the function that cancels it. */
export type Schedule = (run: () => void, ms: number) => () => void;

const workerSource = `
const timers = new Map();
onmessage = (event) => {
  const { id, ms, cancel } = event.data;
  if (cancel !== undefined) {
    clearTimeout(timers.get(cancel));
    timers.delete(cancel);
    return;
  }
  timers.set(id, setTimeout(() => {
    timers.delete(id);
    postMessage(id);
  }, ms));
};
`;

/** Starts the timer Worker, or returns null where Workers or Blob URLs aren't available. */
export function startTimerWorker(): WorkerLike | null {
  try {
    if (
      typeof Worker === "undefined" ||
      typeof URL.createObjectURL !== "function"
    ) {
      return null;
    }
    const url = URL.createObjectURL(
      new Blob([workerSource], { type: "text/javascript" }),
    );
    return new Worker(url);
  } catch {
    // A content security policy without blob: workers, for one.
    return null;
  }
}

const timeoutSchedule: Schedule = (run, ms) => {
  const timer = setTimeout(run, ms);
  return () => clearTimeout(timer);
};

/**
 * A `Schedule` whose timers run in a dedicated Worker, so they keep time in a hidden tab. One
 * Worker serves every timer of the schedule; it starts on the first timer. Where no Worker can
 * start, or it fails, the timers fall back to `setTimeout`, which a hidden tab slows down.
 */
export function workerSchedule(
  options: { createWorker?: () => WorkerLike | null; fallback?: Schedule } = {},
): Schedule {
  const createWorker = options.createWorker ?? startTimerWorker;
  const fallback = options.fallback ?? timeoutSchedule;
  let worker: WorkerLike | null | undefined;
  let nextId = 1;
  const pending = new Map<number, { run: () => void; due: number }>();
  /** Timers moved to the fallback when the Worker failed, by ID, with their cancel. */
  const moved = new Map<number, () => void>();

  function start(): WorkerLike | null {
    if (worker !== undefined) return worker;
    worker = createWorker();
    if (!worker) return null;
    worker.addEventListener("message", (event) => {
      const timer = pending.get(event.data as number);
      if (!timer) return;
      pending.delete(event.data as number);
      timer.run();
    });
    worker.addEventListener("error", () => {
      // The Worker is gone: its timers go on with setTimeout.
      worker = null;
      for (const [id, timer] of pending) {
        moved.set(
          id,
          fallback(
            () => {
              moved.delete(id);
              timer.run();
            },
            Math.max(0, timer.due - Date.now()),
          ),
        );
      }
      pending.clear();
    });
    return worker;
  }

  return (run, ms) => {
    const current = start();
    if (!current) return fallback(run, ms);
    const id = nextId++;
    pending.set(id, { run, due: Date.now() + ms });
    current.postMessage({ id, ms });
    return () => {
      if (pending.delete(id)) {
        worker?.postMessage({ cancel: id });
      } else {
        moved.get(id)?.();
        moved.delete(id);
      }
    };
  };
}
