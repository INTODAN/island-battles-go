import { describe, expect, spyOn, test } from "bun:test";
import { ApiError } from "../api/errors";
import { setServerUrls } from "../platform";
import { playsByHand } from "../play/rules";
import type { Realtime } from "../realtime";
import { createStore } from "../realtime/store";
import { startBotRelays } from ".";
import {
  BotError,
  type GameEndedDelivery,
  type GenerationState,
  type TurnDelivery,
} from "./localhost";
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
    turn_number: generationId,
    max_generations: 300,
    turn_deadline_at: null,
    turn_pace_milliseconds: null,
    ...extra,
  } as GenerationState;
}

/** The server's delivery of the game's current generation. */
function deliveryOf(current: GenerationState): TurnDelivery {
  return {
    turn: {
      game_id: current.game_id,
      generation: current.generation,
      turn_number: current.turn_number,
      max_generations: current.max_generations,
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

/** The server's game-ended delivery of the game's final state. */
function endedOf(current: GenerationState): GameEndedDelivery {
  return {
    game_id: current.game_id,
    team_color: current.team_color,
    turn_number: current.turn_number,
    outcome: current.outcome,
    generation: current.generation,
    memory: current.memory,
    demo: {
      url: `https://island-battles.example.org/game/${current.game_id}/demo?token=ibdt1.x`,
      expires_at: "2026-11-28T12:00:00Z",
    },
  } as GameEndedDelivery;
}

function setup(overrides: Partial<RelayDeps> = {}) {
  const game = createStore<GenerationState | null>(null);
  const time = clock();
  const log = {
    reads: 0,
    deliveries: [] as TurnDelivery[],
    deadlines: 0,
    endReads: 0,
    ends: [] as GameEndedDelivery[],
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
    async loadGameEnded() {
      log.endReads++;
      const current = game.get();
      if (!current?.outcome) {
        throw new ApiError({
          kind: "problem",
          status: 409,
          problemType: "urn:island-battles:problem:game-not-ended",
          detail: "the game has not ended",
        });
      }
      return endedOf(current);
    },
    async deliverGameEnded(delivery) {
      log.ends.push(delivery);
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

    describe("a player who plays by hand", () => {
      const manual = (generationId: number, team = 2) =>
        state(generationId, {
          bot: { kind: "manual" },
          team_color: team,
          turn_pace_milliseconds: 1000,
          turn_deadline_at: new Date(1_000_000 + 1000).toISOString(),
          generation: {
            id: generationId,
            castles: [
              {
                id: 1,
                team_color: 2,
                soldiers: 5,
                burst_debt: 0,
                upgrades: {},
                x: 0,
                y: 0,
              },
            ],
            soldier_groups: [],
            clashes: [],
          },
        });

      test("leaves the deadline to hand play while it stands down", async () => {
        const { game, time, log } = setup({ standDown: playsByHand });
        game.set(manual(1));
        await flush();
        await time.advance(60_000);
        expect(log.deadlines).toBe(0);
      });

      test("keeps the game moving again once the team is out", async () => {
        const { game, time, log } = setup({ standDown: playsByHand });
        game.set(manual(1));
        await flush();
        game.set(manual(2, 5));
        await flush();
        await time.advance(2000);
        expect(log.deadlines).toBe(1);
      });

      test("stands down for a turn whose state changes while it waits", async () => {
        let standing = false;
        const { game, time, log } = setup({ standDown: () => standing });
        game.set(manual(1, 5));
        await flush();
        standing = true;
        await time.advance(60_000);
        expect(log.deadlines).toBe(0);
      });

      test("a relay without hand play keeps today's watch", async () => {
        const { game, time, log } = setup();
        game.set(manual(1));
        await flush();
        await time.advance(2000);
        expect(log.deadlines).toBe(1);
      });
    });
  });

  describe("the end of the game", () => {
    const outcome = {
      reason: "last_team_standing",
      summary: "Red wins",
      winner_team_color: 2,
    } as const;

    test("posts the game-ended delivery to the bot once, however often the final state arrives", async () => {
      const { game, relay, log } = setup();
      game.set(state(1));
      await flush();
      game.set(state(2, { outcome }));
      await flush();
      game.set(state(2, { outcome, log_lines_stored: 3 }));
      await flush();
      expect(log.endReads).toBe(1);
      expect(log.ends).toEqual([endedOf(state(2, { outcome }))]);
      expect(relay.status.get()).toEqual({ kind: "idle" });
    });

    test("tells a bot whose team was out, or that joins at the end, as well", async () => {
      const { game, log } = setup();
      game.set(state(9, { outcome, submitted: true, team_color: 5 }));
      await flush();
      expect(log.reads).toBe(0);
      expect(log.ends).toHaveLength(1);
    });

    test("tells each game's bot once: a later game is told again", async () => {
      const { game, log } = setup();
      game.set(state(2, { outcome }));
      await flush();
      game.set(state(1, { game_id: 8 }));
      await flush();
      game.set(state(3, { game_id: 8, outcome }));
      await flush();
      expect(log.ends.map((ended) => ended.game_id)).toEqual([7, 8]);
    });

    test("tells nothing for a player whose bot isn't a localhost bot", async () => {
      for (const bot of [
        { kind: "default", level: 2 },
        { kind: "manual" },
        { kind: "hosted" },
      ] as GenerationState["bot"][]) {
        const { game, log } = setup();
        game.set(state(2, { outcome, bot }));
        await flush();
        expect(log.endReads).toBe(0);
        expect(log.ends).toHaveLength(0);
      }
    });

    test("a seat's relay tells the seat's bot whatever the host's own bot", async () => {
      const { game, log } = setup({ seat: true });
      game.set(state(2, { outcome, bot: { kind: "default", level: 1 } }));
      await flush();
      expect(log.ends).toHaveLength(1);
    });

    test("ignores every failure quietly, and doesn't ask again", async () => {
      const debug = spyOn(console, "debug").mockImplementation(() => {});
      try {
        const failures: (() => never)[] = [
          () => {
            throw new BotError("unreachable", "not running");
          },
          () => {
            throw new BotError("refused", "HTTP 404");
          },
          () => {
            throw new ApiError({ kind: "transport", status: 0, detail: "x" });
          },
          () => {
            throw new ApiError({
              kind: "problem",
              status: 409,
              problemType: "urn:island-battles:problem:game-not-ended",
              detail: "the game has not ended",
            });
          },
        ];
        for (const [index, fail] of failures.entries()) {
          let reads = 0;
          const { game, relay, time } = setup(
            index < 2
              ? { deliverGameEnded: async () => fail() }
              : {
                  loadGameEnded: async () => {
                    reads++;
                    return fail();
                  },
                },
          );
          game.set(state(2, { outcome }));
          await flush();
          await time.advance(60_000);
          expect(relay.status.get()).toEqual({ kind: "idle" });
          expect(time.pending()).toBe(0);
          if (index >= 2) expect(reads).toBe(1);
        }
        expect(debug).toHaveBeenCalledTimes(failures.length);
      } finally {
        debug.mockRestore();
      }
    });

    test("does not post another game's delivery", async () => {
      const { game, log } = setup({
        loadGameEnded: async () => endedOf(state(2, { game_id: 6, outcome })),
      });
      game.set(state(2, { outcome }));
      await flush();
      expect(log.ends).toHaveLength(0);
    });

    test("startBotRelays reads each bot's delivery, the seats' with player_id, and posts it to its port", async () => {
      const saved = globalThis.fetch;
      const requests: { url: string; method: string; body: string }[] = [];
      globalThis.fetch = (async (
        input: Request | string,
        init?: RequestInit,
      ) => {
        const request = new Request(input, init);
        requests.push({
          url: request.url,
          method: request.method,
          body: await request.text(),
        });
        const url = new URL(request.url);
        if (url.pathname.endsWith("/game-ended-delivery")) {
          const seat = url.searchParams.get("player_id");
          return Response.json({
            ...endedOf(final),
            team_color: seat === null ? 2 : 3,
          });
        }
        return new Response('{"accepted": true}', { status: 202 });
      }) as unknown as typeof fetch;
      const final = state(4, {
        outcome,
        bot: { kind: "localhost", port: 41414 },
        relayed_bots: [
          {
            player_id: "seat-a",
            team_color: 3,
            display_name: "Localhost bot (port 41415)",
            port: 41415,
          },
        ],
      });
      setServerUrls({
        game_url: "http://game.test",
        lobby_url: "http://lobby.test",
      });
      const game = createStore<GenerationState | null>(final);
      const relays = startBotRelays({
        realtime: {
          session: createStore(null),
          game,
        } as unknown as Realtime,
        lobbyId: "lobby",
      });
      try {
        for (let i = 0; i < 50 && requests.length < 4; i++) {
          await new Promise((resolve) => setTimeout(resolve, 2));
        }
        const seen = requests.map(
          (request) => `${request.method} ${request.url}`,
        );
        expect(seen.sort()).toEqual(
          [
            "GET http://game.test/game/from-lobby/lobby/game-ended-delivery",
            "GET http://game.test/game/from-lobby/lobby/game-ended-delivery?player_id=seat-a",
            "POST http://127.0.0.1:41414/game-ended",
            "POST http://127.0.0.1:41415/game-ended",
          ].sort(),
        );
        const posted = (port: number) =>
          JSON.parse(
            requests.find((request) =>
              request.url.startsWith(`http://127.0.0.1:${port}/`),
            )?.body ?? "null",
          );
        expect(posted(41414).team_color).toBe(2);
        expect(posted(41415).team_color).toBe(3);
      } finally {
        relays.dispose();
        setServerUrls({});
        globalThis.fetch = saved;
      }
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
