import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import { decideStartRetry, watchForGame } from "./startRetry";

const problem = (status: number) =>
  new ApiError({
    kind: "problem",
    status,
    code: status === 404 ? "not_found" : undefined,
  });

describe("the start retry decision", () => {
  test("retries 404 and 409 after 1 s, 2 s, 4 s, then every 10 s", () => {
    for (const status of [404, 409]) {
      const delays = [1, 2, 3, 4, 5, 6, 20].map((attempt) =>
        decideStartRetry(problem(status), attempt),
      );
      expect(delays.every((decision) => decision.retry)).toBe(true);
      expect(delays.map((decision) => decision.delayMs)).toEqual([
        1000, 2000, 4000, 10000, 10000, 10000, 10000,
      ]);
    }
  });

  test("retries when the server cannot be reached or fails", () => {
    expect(
      decideStartRetry(new ApiError({ kind: "transport", status: 0 }), 1),
    ).toEqual({
      retry: true,
      delayMs: 1000,
    });
    expect(decideStartRetry(problem(503), 2)).toEqual({
      retry: true,
      delayMs: 2000,
    });
  });

  test("gives up on what waiting will not fix", () => {
    for (const status of [401, 403, 422]) {
      expect(decideStartRetry(problem(status), 1)).toEqual({
        retry: false,
        delayMs: 0,
      });
    }
    expect(decideStartRetry(new Error("bug"), 1).retry).toBe(false);
  });
});

describe("watching for the game", () => {
  function setup(results: (ApiError | "ok")[], host = false) {
    const events: string[] = [];
    const timers: { run: () => void; ms: number; cancelled: boolean }[] = [];
    let call = 0;
    const watcher = watchForGame({
      async probe() {
        const result = results[call++] ?? "ok";
        events.push(result === "ok" ? "probe ok" : `probe ${result.status}`);
        if (result !== "ok") throw result;
      },
      ...(host
        ? {
            async restart() {
              events.push("restart");
              throw problem(502);
            },
          }
        : {}),
      onReady: () => events.push("ready"),
      onFatal: (error) => events.push(`fatal ${(error as ApiError).status}`),
      schedule(run, ms) {
        const timer = { run, ms, cancelled: false };
        timers.push(timer);
        return () => {
          timer.cancelled = true;
        };
      },
    });
    const flush = async () => {
      for (let i = 0; i < 5; i++) await Promise.resolve();
    };
    return { events, timers, watcher, flush };
  }

  test("reads again after each 404 or 409 until the game is there", async () => {
    const { events, timers, flush } = setup([problem(404), problem(409), "ok"]);
    await flush();
    expect(timers.map((timer) => timer.ms)).toEqual([1000]);
    timers[0]?.run();
    await flush();
    expect(timers.map((timer) => timer.ms)).toEqual([1000, 2000]);
    timers[1]?.run();
    await flush();
    expect(events).toEqual(["probe 404", "probe 409", "probe ok", "ready"]);
  });

  test("the host starts the game again after each read that finds none", async () => {
    const { events, timers, flush } = setup(
      [problem(404), problem(409), "ok"],
      true,
    );
    await flush();
    expect(timers.map((timer) => timer.ms)).toEqual([1000]);
    timers[0]?.run();
    await flush();
    timers[1]?.run();
    await flush();
    // A failed restart, such as 502 game_unavailable, waits for the next read;
    // a game being made (409) needs no restart.
    expect(events).toEqual([
      "probe 404",
      "restart",
      "probe 409",
      "probe ok",
      "ready",
    ]);
  });

  test("stops reading when the lobby changes", async () => {
    const { events, timers, watcher, flush } = setup([
      problem(404),
      problem(404),
    ]);
    await flush();
    watcher.stop();
    expect(timers[0]?.cancelled).toBe(true);
    expect(events).toEqual(["probe 404"]);
  });

  test("reports a read that waiting will not fix", async () => {
    const { events, timers, flush } = setup([problem(403)]);
    await flush();
    expect(events).toEqual(["probe 403", "fatal 403"]);
    expect(timers).toHaveLength(0);
  });
});
