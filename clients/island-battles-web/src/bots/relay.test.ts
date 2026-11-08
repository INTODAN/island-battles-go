import { describe, expect, test } from "bun:test";
import { ApiError } from "../api/errors";
import { createStore } from "../realtime/store";
import {
  BotError,
  type BotTurn,
  type BotTurnResult,
  type GenerationState,
  type Upgrade,
} from "./localhost";
import {
  createBotRelay,
  deadlineRetryDelay,
  type RelayDeps,
  submitRetryDelay,
  type TurnSubmission,
} from "./relay";

const upgrades = [{ type: "speed" }] as unknown as Upgrade[];

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
    turns: [] as BotTurn[],
    submissions: [] as TurnSubmission[],
    deadlines: 0,
    upgradeReads: 0,
  };
  let ids = 0;
  const relay = createBotRelay({
    game,
    async loadUpgrades() {
      log.upgradeReads++;
      return upgrades;
    },
    async playBot(turn): Promise<BotTurnResult> {
      log.turns.push(turn);
      return {
        commands: [],
        logs: [
          {
            level: "info",
            logger: "bot",
            message: `turn ${turn.generation.id}`,
          },
        ],
        memory: { seen: turn.generation.id },
      };
    },
    async submit(submission) {
      log.submissions.push(submission);
    },
    async applyDeadline() {
      log.deadlines++;
    },
    now: time.now,
    newRequestId: () => `request-${++ids}`,
    schedule: time.schedule,
    ...overrides,
  });
  return { game, relay, time, log };
}

describe("the bot relay", () => {
  test("plays a new generation: builds the BotTurn, submits the result complete with the memory", async () => {
    const { game, relay, log } = setup();
    const deadline = new Date(1_000_000 + 30_000).toISOString();
    game.set(state(3, { turn_deadline_at: deadline }));
    await flush();

    expect(log.turns).toHaveLength(1);
    const turn = log.turns[0];
    expect(turn?.game_id).toBe(7);
    expect(turn?.team_color).toBe(2);
    expect(turn?.memory).toEqual({ seen: 2 });
    expect(turn?.turn_deadline_at).toBe(deadline);
    expect(turn?.upgrades).toBe(upgrades);
    expect(log.submissions).toEqual([
      {
        game_id: 7,
        generation_id: 3,
        request_id: "request-1",
        commands: [],
        logs: [{ level: "info", logger: "bot", message: "turn 3" }],
        memory: { seen: 3 },
        complete: true,
      },
    ]);
    expect(relay.status.get()).toEqual({ kind: "submitted", generationId: 3 });
  });

  test("reads the upgrade table once for the whole game", async () => {
    const { game, log } = setup();
    game.set(state(1));
    await flush();
    game.set(state(2));
    await flush();
    game.set(state(3));
    await flush();
    expect(log.turns).toHaveLength(3);
    expect(log.upgradeReads).toBe(1);
  });

  test("ignores a repeated state of the same generation and plays each generation once", async () => {
    const { game, log } = setup();
    game.set(state(1));
    await flush();
    game.set(state(1, { log_lines_stored: 4 }));
    await flush();
    expect(log.turns).toHaveLength(1);
    expect(log.submissions).toHaveLength(1);
  });

  test("submits nothing for a default bot", async () => {
    const { game, relay, log } = setup();
    game.set(state(1, { bot: { kind: "default", level: 2 } }));
    await flush();
    expect(log.turns).toHaveLength(0);
    expect(log.submissions).toHaveLength(0);
    expect(relay.status.get()).toEqual({ kind: "idle" });
  });

  test("does not play a turn the player already completed", async () => {
    const { game, relay, log } = setup();
    game.set(state(4, { submitted: true }));
    await flush();
    expect(log.turns).toHaveLength(0);
    expect(relay.status.get()).toEqual({ kind: "submitted", generationId: 4 });
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
    expect(log.turns).toHaveLength(1);
    expect(relay.status.get()).toEqual({ kind: "idle" });
  });

  test("shows a bot that crashed and plays again on retry, with a fresh request", async () => {
    let fail = true;
    const { game, relay, log } = setup({
      async playBot() {
        if (fail)
          throw new BotError("crashed", "Your bot failed: ZeroDivisionError");
        return { commands: [], logs: [], memory: null };
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toEqual({
      kind: "error",
      generationId: 1,
      message: "Your bot failed: ZeroDivisionError",
      retrying: false,
    });
    expect(log.submissions).toHaveLength(0);

    fail = false;
    relay.retry();
    await flush();
    expect(log.submissions).toHaveLength(1);
    expect(log.submissions[0]?.memory).toBeNull();
    expect(relay.status.get().kind).toBe("submitted");
  });

  test("keeps asking a bot that is not running yet", async () => {
    let up = false;
    const { game, relay, time, log } = setup({
      async playBot() {
        if (!up)
          throw new BotError("unreachable", "Your bot is not reachable.");
        return { commands: [], logs: [], memory: null };
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toMatchObject({ kind: "error", retrying: true });

    up = true;
    await time.advance(2000);
    expect(log.submissions).toHaveLength(1);
    expect(relay.status.get().kind).toBe("submitted");
  });

  test("resends a submission lost to the network with the same request id", async () => {
    let failures = 2;
    const attempts: string[] = [];
    const { game, relay, time } = setup({
      async submit(submission) {
        attempts.push(submission.request_id);
        if (failures-- > 0) {
          throw new ApiError({
            kind: "transport",
            status: 0,
            detail: "offline",
          });
        }
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toMatchObject({ kind: "error", retrying: true });
    await time.advance(submitRetryDelay(0));
    await time.advance(submitRetryDelay(1));
    expect(attempts).toEqual(["request-1", "request-1", "request-1"]);
    expect(relay.status.get().kind).toBe("submitted");
  });

  test("shows a rejected result and does not resend it", async () => {
    const { game, relay, time } = setup({
      async submit() {
        throw new ApiError({
          kind: "problem",
          status: 409,
          problemType: "urn:island-battles:problem:insufficient-soldiers",
          detail: "castle 3 has 12 unreserved soldiers; speed level 3 costs 20",
        });
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toEqual({
      kind: "error",
      generationId: 1,
      message: "castle 3 has 12 unreserved soldiers; speed level 3 costs 20",
      retrying: false,
    });
    expect(time.pending()).toBe(0);
  });

  test("treats a submission after the game ended as no error", async () => {
    const { game, relay } = setup({
      async submit() {
        throw new ApiError({
          kind: "problem",
          status: 409,
          problemType: "urn:island-battles:problem:game-ended",
        });
      },
    });
    game.set(state(1));
    await flush();
    expect(relay.status.get()).toEqual({ kind: "idle" });
  });

  test("a new generation drops the previous turn's retries", async () => {
    let up = false;
    const { game, time, log } = setup({
      async playBot(turn) {
        if (!up) throw new BotError("unreachable", "down");
        return { commands: [], logs: [], memory: { seen: turn.generation.id } };
      },
    });
    game.set(state(1));
    await flush();
    up = true;
    game.set(state(2));
    await flush();
    await time.advance(10_000);
    expect(log.submissions.map((item) => item.generation_id)).toEqual([2]);
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

    test("does not call the deadline before this player has submitted", async () => {
      const { game, time, log } = setup({
        async playBot() {
          throw new BotError("crashed", "boom");
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
    expect(log.turns).toHaveLength(1);
    expect(log.deadlines).toBe(0);
  });
});
