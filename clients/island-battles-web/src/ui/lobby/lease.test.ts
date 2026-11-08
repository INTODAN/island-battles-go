import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import { heartbeatIntervalMs, releaseLease, startHeartbeat } from "./lease";

function fakeTimers() {
  const timers: { run: () => void; ms: number; cancelled: boolean }[] = [];
  return {
    timers,
    schedule(run: () => void, ms: number) {
      const timer = { run, ms, cancelled: false };
      timers.push(timer);
      return () => {
        timer.cancelled = true;
      };
    },
    /** Runs the newest live timer. */
    async fire() {
      const timer = [...timers].reverse().find((item) => !item.cancelled);
      timer?.run();
      for (let i = 0; i < 5; i++) await Promise.resolve();
    },
  };
}

describe("the lease heartbeat", () => {
  test("beats every 15 seconds", async () => {
    const { timers, schedule, fire } = fakeTimers();
    let beats = 0;
    startHeartbeat({
      heartbeat: async () => {
        beats++;
      },
      onLost: () => {},
      schedule,
    });
    expect(timers.map((timer) => timer.ms)).toEqual([heartbeatIntervalMs]);
    await fire();
    await fire();
    expect(beats).toBe(2);
    expect(timers.map((timer) => timer.ms)).toEqual([15000, 15000, 15000]);
  });

  test("keeps beating after a failed beat", async () => {
    const { timers, schedule, fire } = fakeTimers();
    startHeartbeat({
      heartbeat: async () => {
        throw new ApiError({ kind: "transport", status: 0 });
      },
      onLost: () => {
        throw new Error("lost");
      },
      schedule,
    });
    await fire();
    expect(timers).toHaveLength(2);
  });

  test("stops and says so when the membership is stale", async () => {
    const { timers, schedule, fire } = fakeTimers();
    const lost: ApiError[] = [];
    startHeartbeat({
      heartbeat: async () => {
        throw new ApiError({
          kind: "problem",
          status: 409,
          code: "stale_membership",
        });
      },
      onLost: (error) => lost.push(error),
      schedule,
    });
    await fire();
    expect(lost).toHaveLength(1);
    expect(timers).toHaveLength(1);
  });

  test("stop cancels the next beat", () => {
    const { timers, schedule } = fakeTimers();
    const lease = startHeartbeat({
      heartbeat: async () => {},
      onLost: () => {},
      schedule,
    });
    lease.stop();
    expect(timers[0]?.cancelled).toBe(true);
  });
});

describe("releasing the lease", () => {
  test("a keepalive DELETE with the generation and the token", () => {
    let seen: { url: string; init: RequestInit | undefined } | undefined;
    releaseLease(
      {
        gameKey: "island-battles",
        lobbyId: "lobby-1",
        clientInstanceId: "tab-1",
        membershipGeneration: 3,
        accessToken: "secret",
      },
      (async (url: string, init?: RequestInit) => {
        seen = { url, init };
        return new Response(null, { status: 204 });
      }) as unknown as typeof fetch,
    );
    expect(seen?.url).toBe(
      "/lobby/v1/games/island-battles/lobbies/lobby-1/leases/tab-1?generation=3",
    );
    expect(seen?.init?.method).toBe("DELETE");
    expect(seen?.init?.keepalive).toBe(true);
    expect(seen?.init?.headers).toEqual({ Authorization: "Bearer secret" });
  });
});
