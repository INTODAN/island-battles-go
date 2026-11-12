import { describe, expect, test } from "bun:test";
import { ApiError } from "../api/errors";
import { createStore } from "../realtime/store";
import { BotError, type GenerationState, type TurnDelivery } from "./localhost";
import {
  createBotRelay,
  deadlineRetryDelay,
  type RelayDeps,
  readRetryDelay,
} from "./relay";

function state(
  generationId: number,
  extra: Partial<GenerationState> = {},
): GenerationState {
  return {
    bot: { kind: "localhost" },
    channel: "player:p1",
    game_id: 7,
    generation: {
      id: generationId,
      castles: [],
      soldier_groups: [],
      clashes: [],
    },
    generation_id: generationId,
    lobby_id: "lobby",
    log_lines_stored: 0,
    memory: { seen: generationId - 1 },
    player_id: "p1",
    submitted: false,
    team_color: 2,
    turn_deadline_at: null,
    ...extra,
  } as GenerationState;
}

/** The server's delivery of the game's current generation. */
function deliveryOf(current: GenerationState): TurnDelivery {
  return {
    turn: {
      game_id: current.game_id,
      generation: current.generation,
      team_color: current.team_color,
      memory: current.memory,
      upgrades: [],
      turn_deadline_at: current.turn_deadline_at,
    },
    submit: {
      url: "http://localhost:8888/generation/commands",
      token: `ibst1.${current.generation_id}`,
      deadline_at: current.turn_deadline_at,
    },
  } as TurnDelivery;
}

/** A fake clock whose timers run only when the test advances it. */
function clock() {
  let now = 1_000_000;
  let nextId = 1;
  const timers = new Map<number, { at: number; run: () => void }>();
  return {
    now: () => now,
    schedule(run: () => void, ms: number) {
      const id = nextId++;
      timers.set(id, { at: now + ms, run });
      return () => {
        timers.delete(id);
      };
    },
    async advance(ms: number) {
      const target = now + ms;
      for (;;) {
        const due = [...timers.entries()]
          .filter(([, timer]) => timer.at <= target)
          .sort((a, b) => a[1].at - b[1].at)[0];
        if (!due) break;
        timers.delete(due[0]);
        now = Math.max(now, due[1].at);
        due[1].run();
        await flush();
      }
      now = target;
    },
    pending: () => timers.size,
  };
}

const flush = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve();
};

function setup(overrides: Partial<RelayDeps> = {}) {
  const game = createStore<GenerationState | null>(null);
  const time = clock();
  const log = {
    reads: 0,
    deliveries: [] as TurnDelivery[],
    deadlines: 0,
  };
  const relay = createBotRelay({
    game,
    async loadDelivery() {
      log.reads++;
      const current = game.get();
      if (!current) throw new Error("no game");
      return deliveryOf(current);
    },
    async deliver(delivery) {
      log.deliveries.push(delivery);
    },
    async applyDeadline() {
      log.deadlines++;
    },
    now: time.now,
    schedule: time.schedule,
    ...overrides,
  });
  return { game, relay, time, log };
}

const conflict = (problemType?: string) =>
  new ApiError({
    kind: "problem",
    status: 409,
    problemType,
    detail: "player turn is already complete",
  });

describe("the bot relay", () => {
  test("delivers a new generation's turn, as the server built it, to the bot", async () => {
    const { game, relay, log } = setup();
    const deadline = new Date(1_000_000 + 30_000).toISOString();
    game.set(state(3, { turn_deadline_at: deadline }));
    await flush();

    expect(log.reads).toBe(1);
    expect(log.deliveries).toEqual([
      deliveryOf(state(3, { turn_deadline_at: deadline })),
    ]);
    expect(relay.status.get()).toEqual({ kind: "delivered", generationId: 3 });
  });

  test("ignores a repeated state of the same generation and delivers each generation once", async () => {
    const { game, log } = setup();
    game.set(state(1));
    await flush();
    game.set(state(1, { log_lines_stored: 4 }));
    await flush();
    expect(log.deliveries).toHaveLength(1);
    game.set(state(2));
    await flush();
    expect(log.deliveries.map((item) => item.turn.generation.id)).toEqual([
      1, 2,
    ]);
  });

  test("delivers nothing for a default bot", async () => {
    const { game, relay, log } = setup();
    game.set(state(1, { bot: { kind: "default", level: 2 } }));
    await flush();
    expect(log.reads).toBe(0);
    expect(log.deliveries).toHaveLength(0);
    expect(relay.status.get()).toEqual({ kind: "idle" });
  });

  test("does not deliver a turn the player already completed", async () => {
    const { game, relay, log } = setup();
    game.set(state(4, { submitted: true }));
    await flush();
    expect(log.reads).toBe(0);
    expect(relay.status.get()).toEqual({ kind: "delivered", generationId: 4 });
  });

  test("a turn with nothing to play waits for the others", async () => {
    const { game, relay, log } = setup({
      async loadDelivery() {
        throw conflict();
      },
    });
    game.set(state(1));
    await flush();
    expect(log.deliveries).toHaveLength(0);
    expect(relay.status.get()).toEqual({ kind: "delivered", generationId: 1 });
  });

  test("a game that ended while reading the delivery is no error", async () => {
    const { game, relay } = setup({
      async loadDelivery() {
        throw conflict("urn:island-battles:problem:game-ended");
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toEqual({ kind: "idle" });
  });

  test("does not deliver another generation's turn", async () => {
    const { game, log } = setup({
      async loadDelivery() {
        return deliveryOf(state(2));
      },
    });
    game.set(state(1));
    await flush();
    expect(log.deliveries).toHaveLength(0);
  });

  test("stops at the outcome", async () => {
    const { game, relay, log } = setup();
    game.set(state(1));
    await flush();
    game.set(
      state(2, {
        outcome: {
          reason: "last_team_standing",
          summary: "Red wins",
          winner_team_color: 2,
        },
      }),
    );
    await flush();
    expect(log.deliveries).toHaveLength(1);
    expect(relay.status.get()).toEqual({ kind: "idle" });
  });

  test("shows a bot that refused the turn and delivers again on retry", async () => {
    let refuse = true;
    const { game, relay, log, time } = setup({
      async deliver(delivery) {
        log.deliveries.push(delivery);
        if (refuse)
          throw new BotError(
            "refused",
            "Your bot refused the turn (HTTP 400). submit.url is not an allowed host",
          );
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toEqual({
      kind: "error",
      generationId: 1,
      message:
        "Your bot refused the turn (HTTP 400). submit.url is not an allowed host",
      retrying: false,
    });
    expect(time.pending()).toBe(0);

    refuse = false;
    relay.retry();
    await flush();
    expect(log.deliveries).toHaveLength(2);
    expect(log.reads).toBe(2);
    expect(relay.status.get().kind).toBe("delivered");
  });

  test("keeps asking a bot that is not running yet", async () => {
    let up = false;
    const { game, relay, time, log } = setup({
      async deliver(delivery) {
        if (!up)
          throw new BotError("unreachable", "Your bot is not reachable.");
        log.deliveries.push(delivery);
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toMatchObject({ kind: "error", retrying: true });
    await time.advance(1999);
    expect(log.deliveries).toHaveLength(0);

    up = true;
    await time.advance(1);
    expect(log.deliveries).toHaveLength(1);
    expect(relay.status.get().kind).toBe("delivered");
  });

  test("reads the delivery again after a server failure", async () => {
    let failures = 2;
    const { game, relay, time, log } = setup({
      async loadDelivery() {
        log.reads++;
        if (failures-- > 0) {
          throw new ApiError({
            kind: "transport",
            status: 0,
            detail: "offline",
          });
        }
        return deliveryOf(state(1));
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toMatchObject({ kind: "error", retrying: true });
    await time.advance(readRetryDelay(0));
    await time.advance(readRetryDelay(1));
    expect(log.reads).toBe(3);
    expect(log.deliveries).toHaveLength(1);
    expect(relay.status.get().kind).toBe("delivered");
    expect([0, 1, 2, 3, 4].map(readRetryDelay)).toEqual([
      1000, 2000, 4000, 8000, 8000,
    ]);
  });

  test("a new generation drops the previous turn's retries", async () => {
    let up = false;
    const { game, time, log } = setup({
      async deliver(delivery) {
        if (!up) throw new BotError("unreachable", "down");
        log.deliveries.push(delivery);
      },
    });
    game.set(state(1));
    await flush();
    up = true;
    game.set(state(2));
    await flush();
    await time.advance(10_000);
    expect(log.deliveries.map((item) => item.turn.generation.id)).toEqual([2]);
  });

  describe("turn deadlines", () => {
    test("asks the server to apply the deadline a second after it, while waiting for others", async () => {
      const { game, time, log } = setup();
      game.set(
        state(1, {
          turn_deadline_at: new Date(1_000_000 + 30_000).toISOString(),
        }),
      );
      await flush();
      await time.advance(30_999);
      expect(log.deadlines).toBe(0);
      await time.advance(2);
      expect(log.deadlines).toBe(1);
    });

    test("asks again after 2 s, doubling up to 30 s, while the turn stays open", async () => {
      const { game, time, log } = setup();
      game.set(
        state(1, { turn_deadline_at: new Date(1_000_000).toISOString() }),
      );
      await flush();
      await time.advance(1000);
      expect(log.deadlines).toBe(1);
      await time.advance(deadlineRetryDelay(0));
      expect(log.deadlines).toBe(2);
      await time.advance(deadlineRetryDelay(1));
      expect(log.deadlines).toBe(3);
      expect([0, 1, 2, 3, 4, 5, 6].map(deadlineRetryDelay)).toEqual([
        2000, 4000, 8000, 16000, 30000, 30000, 30000,
      ]);
    });

    test("stops when a new generation arrives", async () => {
      const { game, time, log } = setup();
      game.set(
        state(1, {
          turn_deadline_at: new Date(1_000_000 + 5000).toISOString(),
        }),
      );
      await flush();
      game.set(state(2));
      await flush();
      await time.advance(60_000);
      expect(log.deadlines).toBe(0);
    });

    test("does not call the deadline while the bot refuses the turn", async () => {
      const { game, time, log } = setup({
        async deliver() {
          throw new BotError("refused", "no");
        },
      });
      game.set(
        state(1, {
          turn_deadline_at: new Date(1_000_000 + 1000).toISOString(),
        }),
      );
      await flush();
      await time.advance(60_000);
      expect(log.deadlines).toBe(0);
    });

    test("a default-bot player still keeps the game moving", async () => {
      const { game, time, log } = setup();
      game.set(
        state(1, {
          bot: { kind: "default", level: 1 },
          turn_deadline_at: new Date(1_000_000 + 1000).toISOString(),
        }),
      );
      await flush();
      await time.advance(2000);
      expect(log.deadlines).toBe(1);
    });
  });

  test("dispose stops everything", async () => {
    const { game, relay, time, log } = setup();
    game.set(
      state(1, { turn_deadline_at: new Date(1_000_000 + 1000).toISOString() }),
    );
    await flush();
    relay.dispose();
    game.set(state(2));
    await flush();
    await time.advance(60_000);
    expect(log.deliveries).toHaveLength(1);
    expect(log.deadlines).toBe(0);
  });
});
