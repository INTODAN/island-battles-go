import { describe, expect, test } from "bun:test";
import { type Schedule, type WorkerLike, workerSchedule } from "./ticker";

/** A fake Worker: the test fires its timers by ID. */
function fakeWorker() {
  const messages: unknown[] = [];
  const listeners: Record<string, ((event: { data?: unknown }) => void)[]> = {
    message: [],
    error: [],
  };
  const worker: WorkerLike = {
    postMessage(message) {
      messages.push(message);
    },
    addEventListener(type, listener) {
      listeners[type]?.push(listener);
    },
  };
  return {
    worker,
    messages,
    post: (id: number) => {
      for (const listener of listeners.message ?? []) listener({ data: id });
    },
    fail: () => {
      for (const listener of listeners.error ?? []) listener({});
    },
  };
}

/** A fallback schedule that records its timers. */
function fallback() {
  const timers: { run: () => void; ms: number; cancelled: boolean }[] = [];
  const schedule: Schedule = (run, ms) => {
    const timer = { run, ms, cancelled: false };
    timers.push(timer);
    return () => {
      timer.cancelled = true;
    };
  };
  return { schedule, timers };
}

describe("workerSchedule", () => {
  test("runs each timer when the Worker posts its ID", () => {
    const fake = fakeWorker();
    let created = 0;
    const schedule = workerSchedule({
      createWorker: () => {
        created++;
        return fake.worker;
      },
    });
    const ran: string[] = [];
    schedule(() => ran.push("a"), 100);
    schedule(() => ran.push("b"), 50);
    expect(created).toBe(1);
    expect(fake.messages).toEqual([
      { id: 1, ms: 100 },
      { id: 2, ms: 50 },
    ]);
    fake.post(2);
    fake.post(1);
    fake.post(1);
    expect(ran).toEqual(["b", "a"]);
  });

  test("a cancelled timer tells the Worker and never runs", () => {
    const fake = fakeWorker();
    const schedule = workerSchedule({ createWorker: () => fake.worker });
    let ran = false;
    const cancel = schedule(() => {
      ran = true;
    }, 100);
    cancel();
    fake.post(1);
    expect(ran).toBe(false);
    expect(fake.messages.at(-1)).toEqual({ cancel: 1 });
  });

  test("without a Worker, timers use the fallback", () => {
    const backup = fallback();
    const schedule = workerSchedule({
      createWorker: () => null,
      fallback: backup.schedule,
    });
    schedule(() => {}, 30);
    expect(backup.timers.map((timer) => timer.ms)).toEqual([30]);
  });

  test("a Worker that fails hands its timers to the fallback", () => {
    const fake = fakeWorker();
    const backup = fallback();
    const schedule = workerSchedule({
      createWorker: () => fake.worker,
      fallback: backup.schedule,
    });
    let ran = 0;
    schedule(() => ran++, 0);
    const cancel = schedule(() => ran++, 0);
    fake.fail();
    expect(backup.timers).toHaveLength(2);
    cancel();
    expect(backup.timers[1]?.cancelled).toBe(true);
    backup.timers[0]?.run();
    expect(ran).toBe(1);
    schedule(() => {}, 5);
    expect(backup.timers).toHaveLength(3);
  });
});
