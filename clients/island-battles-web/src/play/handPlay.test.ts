import { describe, expect, test } from "bun:test";
import { ApiError } from "../api/errors";
import { type GenerationState, nextGeneration } from "../realtime/install";
import { createStore } from "../realtime/store";
import {
  createHandPlay,
  type HandPlayDeps,
  type PlayOrder,
  type RegisterGenerationCommands,
  retryDelay,
} from "./handPlay";
import type { UpgradeTable } from "./rules";
import { fakeClock, flush, handState, upgradeTable } from "./testing";

interface Call<T> {
  signal: AbortSignal;
  at: number;
  resolve(value: T): void;
  reject(error: unknown): void;
}

const stale = (typed: boolean) =>
  new ApiError({
    kind: "problem",
    status: 409,
    problemType: typed
      ? "urn:island-battles:problem:stale-generation"
      : undefined,
    detail: "generation is no longer current",
  });
const transport = () => new ApiError({ kind: "transport", status: 0 });
const problem = (status: number, type: string) =>
  new ApiError({
    kind: "problem",
    status,
    problemType: `urn:island-battles:problem:${type}`,
  });

/** A hand player's state of generation `id` whose castle 1 has `soldiers`. */
function withCastle1(
  id: number,
  soldiers: number,
  team = 1,
  extra: Partial<GenerationState> = {},
  createdAt?: number,
) {
  const state = handState(id, extra, createdAt);
  const castle = state.generation.castles[0];
  if (castle) {
    castle.soldiers = soldiers;
    castle.team_color = team;
  }
  return state;
}

function setup(
  options: {
    initial?: GenerationState | null;
    table?: UpgradeTable;
  } & Partial<HandPlayDeps> = {},
) {
  const game = createStore<GenerationState | null>(options.initial ?? null);
  const clock = fakeClock();
  const posts: (Call<GenerationState> & {
    body: RegisterGenerationCommands;
  })[] = [];
  const deadlines: Call<GenerationState>[] = [];
  const reads: Call<GenerationState>[] = [];
  const log = { upgrades: 0, refreshes: 0, ids: 0 };
  function call<T>(list: Call<T>[], signal: AbortSignal, extra = {}) {
    return new Promise<T>((resolve, reject) => {
      list.push({ signal, at: clock.now(), resolve, reject, ...extra });
    });
  }
  const adopt = (state: GenerationState) => {
    const next = nextGeneration(game.get(), state);
    if (next) game.set(next);
  };
  const play = createHandPlay({
    game,
    postCommands: (body, signal) =>
      call(posts as Call<GenerationState>[], signal, { body }),
    applyDeadline: (signal) => call(deadlines, signal),
    readState: (signal) => call(reads, signal),
    async loadUpgrades() {
      log.upgrades++;
      return options.table ?? upgradeTable;
    },
    adopt,
    refresh: () => {
      log.refreshes++;
    },
    now: clock.now,
    schedule: clock.schedule,
    newRequestId: () => `request-${++log.ids}`,
    ...options,
  });
  const settled: PlayOrder[] = [];
  play.onSettled((order) => settled.push(order));
  return { play, game, clock, posts, deadlines, reads, log, settled, adopt };
}

/** Answers a post with the open state, as the server does when it accepts the batch. */
const accept = (env: ReturnType<typeof setup>, index: number) =>
  env.posts[index]?.resolve(env.game.get() as GenerationState);

describe("status", () => {
  test("idle and silent for a player who doesn't play by hand", async () => {
    const env = setup({
      initial: handState(10, { bot: { kind: "default", level: 2 } }),
    });
    expect(env.play.state.get().status).toBe("idle");
    const refused = env.play.send(1, 4, 5);
    expect(refused).toMatchObject({
      ok: false,
      sentence: "The game hasn't started yet.",
    });
    await env.clock.advance(10_000);
    expect(env.posts).toHaveLength(0);
    expect(env.deadlines).toHaveLength(0);
    expect(env.log.upgrades).toBe(0);
  });

  test("playing, eliminated and ended, each refusing orders with its sentence", async () => {
    const env = setup({ initial: handState(10) });
    await flush();
    expect(env.play.state.get()).toMatchObject({
      status: "playing",
      teamColor: 1,
      upgrades: upgradeTable,
    });
    expect([...env.play.state.get().castles.keys()]).toEqual([1, 2]);
    env.game.set(handState(11, { team_color: 5 }));
    expect(env.play.state.get().status).toBe("eliminated");
    expect(env.play.send(1, 4, 1)).toMatchObject({
      ok: false,
      sentence: "Your team is out of the game.",
    });
    env.game.set(
      handState(12, {
        outcome: {
          reason: "last_team_standing",
          summary: "",
          winner_team_color: 1,
        },
      }),
    );
    expect(env.play.state.get().status).toBe("ended");
    expect(env.play.upgrade(1, "speed")).toMatchObject({
      ok: false,
      sentence: "The game has ended.",
    });
  });

  test("the upgrade table is read once", async () => {
    const env = setup({ initial: handState(10) });
    env.game.set(handState(11));
    env.game.set(handState(12));
    await flush();
    expect(env.log.upgrades).toBe(1);
  });
});

describe("orders", () => {
  test("each order is its own batch, not completing, for the open generation", async () => {
    const env = setup({ initial: handState(10) });
    const given = env.play.send(1, 4, 5);
    expect(given).toMatchObject({
      ok: true,
      order: { id: 1, state: "sending", soldiers: 5, generationId: 10 },
    });
    expect(env.posts[0]?.body).toEqual({
      game_id: 7,
      generation_id: 10,
      request_id: "request-1",
      complete: false,
      commands: [
        {
          command_type: "send_soldier_group",
          parameters: {
            origin_castle_id: 1,
            destination_castle_id: 4,
            soldier_amount: 5,
          },
        },
      ],
    });
    env.play.upgrade(1, "speed");
    env.play.burst(2, { share: 1 });
    accept(env, 0);
    await flush();
    expect(env.posts[1]?.body.commands).toEqual([
      {
        command_type: "upgrade_castle",
        parameters: { castle_id: 1, upgrade_type: "speed" },
      },
    ]);
    accept(env, 1);
    await flush();
    expect(env.posts[2]?.body.commands).toEqual([
      {
        command_type: "burst_castle",
        parameters: { castle_id: 2, soldier_amount: 4 },
      },
    ]);
    expect(new Set(env.posts.map((post) => post.body.request_id)).size).toBe(3);
  });

  test("one request at a time, in order", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    env.play.send(1, 3, 6);
    await flush();
    expect(env.posts).toHaveLength(1);
    accept(env, 0);
    await flush();
    expect(env.posts).toHaveLength(2);
    expect(env.posts[1]?.body.commands[0]).toMatchObject({
      parameters: { destination_castle_id: 3, soldier_amount: 6 },
    });
  });

  test("an accepted send lowers the castle's unreserved soldiers for the next order", async () => {
    const env = setup({ initial: handState(10) });
    const first = env.play.send(1, 4, 10);
    expect(env.play.state.get().castles.get(1)?.unreserved).toBe(20);
    accept(env, 0);
    await flush();
    expect(first.ok && (await first.settled).state).toBe("accepted");
    expect(env.play.state.get().castles.get(1)).toMatchObject({
      soldiers: 30,
      unreserved: 20,
    });
    expect(env.play.send(1, 4, 25)).toMatchObject({
      ok: false,
      refusal: { kind: "insufficient-soldiers", unreserved: 20, needed: 25 },
      sentence: "The castle has only 20 soldiers left to send.",
    });
    expect(
      env.play.check({ kind: "send", origin: 1, destination: 4, amount: 20 }),
    ).toEqual({
      ok: true,
      order: { kind: "send", origin: 1, destination: 4, soldiers: 20 },
    });
  });

  test("a rejected order gives its soldiers back", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 10);
    env.posts[0]?.reject(problem(409, "insufficient-soldiers"));
    await flush();
    expect(env.play.state.get().castles.get(1)?.unreserved).toBe(30);
    expect(env.settled).toEqual([
      expect.objectContaining({
        id: 1,
        state: "rejected",
        sentence: "The castle doesn't have enough soldiers left for that.",
      }),
    ]);
    expect(env.play.state.get().orders).toEqual([
      expect.objectContaining({ id: 1, state: "rejected" }),
    ]);
  });

  test("a share is resolved when the order is sent", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(2, 4, 3);
    const all = env.play.send(1, 4, { share: 1 });
    expect(all.ok && all.order.soldiers).toBe(30);
    // Generation 11 arrives while the first order is out: castle 1 has grown.
    env.game.set(withCastle1(11, 41));
    accept(env, 0);
    await flush();
    expect(env.posts[1]?.body).toMatchObject({
      generation_id: 11,
      commands: [{ parameters: { soldier_amount: 41 } }],
    });
  });

  test("a transport failure sends the same body with the same request ID", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    env.posts[0]?.reject(transport());
    await env.clock.advance(99);
    expect(env.posts).toHaveLength(1);
    await env.clock.advance(1);
    expect(env.posts).toHaveLength(2);
    env.posts[1]?.reject(new ApiError({ kind: "problem", status: 503 }));
    await env.clock.advance(200);
    expect(env.posts).toHaveLength(3);
    expect(env.posts[2]?.body).toEqual(
      env.posts[0]?.body as RegisterGenerationCommands,
    );
    accept(env, 2);
    await flush();
    expect(env.settled.map((order) => order.state)).toEqual(["accepted"]);
  });

  test("a request with no answer times out, is aborted and sent again", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    await env.clock.advance(2000);
    expect(env.posts[0]?.signal.aborted).toBe(true);
    await env.clock.advance(100);
    expect(env.posts).toHaveLength(2);
    expect(env.posts[1]?.body.request_id).toBe("request-1");
    // The aborted request's late answer changes nothing.
    accept(env, 0);
    await flush();
    expect(env.settled).toHaveLength(0);
  });

  test("after 5 s without an answer, the order and those behind it are dropped", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    env.play.send(1, 3, 5);
    env.play.upgrade(2, "speed");
    let answered = 0;
    while (env.settled.length === 0 && env.clock.now() < 1_010_000) {
      while (answered < env.posts.length)
        env.posts[answered++]?.reject(transport());
      await env.clock.advance(10);
    }
    expect(
      env.posts.every((post) => post.body.request_id === "request-1"),
    ).toBe(true);
    // Sent at 0, 100, 300, 700, 1500, 2500, 3500 and 4500 ms; the next would be past 5 s.
    expect(env.posts.map((post) => post.at - 1_000_000)).toEqual([
      0, 100, 300, 700, 1500, 2500, 3500, 4500,
    ]);
    expect(
      env.settled.map((order) => [order.id, order.state, order.sentence]),
    ).toEqual([
      [
        1,
        "rejected",
        "The server didn't answer; the order may not have been given.",
      ],
      [
        2,
        "rejected",
        "The server didn't answer; the order may not have been given.",
      ],
      [
        3,
        "rejected",
        "The server didn't answer; the order may not have been given.",
      ],
    ]);
    // The next order goes out.
    env.play.send(1, 4, 1);
    expect(env.posts.at(-1)?.body.request_id).toBe("request-2");
  });

  for (const typed of [true, false]) {
    const shape = typed ? "the stale-generation type" : "a 409 without a type";
    test(`a turn that ended (${shape}) sends the order again against the new generation`, async () => {
      const env = setup({ initial: handState(10) });
      env.play.send(1, 4, { share: 0.5 });
      // The request that is refused plays the turn and pushes the next generation first.
      env.game.set(withCastle1(11, 40));
      env.posts[0]?.reject(stale(typed));
      await flush();
      expect(env.reads).toHaveLength(0);
      expect(env.posts[1]?.body).toMatchObject({
        generation_id: 11,
        request_id: "request-2",
        commands: [{ parameters: { soldier_amount: 20 } }],
      });
      accept(env, 1);
      await flush();
      expect(env.settled).toEqual([
        expect.objectContaining({ state: "accepted", generationId: 11 }),
      ]);
    });
  }

  test("a turn that ended, with no newer state yet, reads it", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    env.posts[0]?.reject(stale(true));
    await flush();
    expect(env.reads).toHaveLength(1);
    env.reads[0]?.resolve(handState(11));
    await flush();
    expect(env.posts[1]?.body).toMatchObject({ generation_id: 11 });
  });

  test("a turn that ended, and the order no longer passes", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    env.game.set(withCastle1(11, 40, 2));
    env.posts[0]?.reject(stale(false));
    await flush();
    expect(env.posts).toHaveLength(1);
    expect(env.settled).toEqual([
      expect.objectContaining({
        state: "rejected",
        sentence: "That castle isn't yours.",
      }),
    ]);
  });

  test("a second ended turn drops the order", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    env.game.set(handState(11));
    env.posts[0]?.reject(stale(true));
    await flush();
    env.game.set(handState(12));
    env.posts[1]?.reject(stale(true));
    await flush();
    expect(env.posts).toHaveLength(2);
    expect(env.settled).toEqual([
      expect.objectContaining({
        state: "rejected",
        sentence: "The turn ended before the order arrived.",
      }),
    ]);
  });

  test("a refusal with a problem type drops the order, and the next one goes", async () => {
    const env = setup({ initial: handState(10) });
    const burst = env.play.burst(2, 2);
    env.play.send(1, 4, 5);
    env.posts[0]?.reject(problem(409, "burst-unavailable"));
    await flush();
    expect(burst.ok && (await burst.settled)).toMatchObject({
      state: "rejected",
      sentence:
        "This castle can't burst now: it is repaying a burst or already bursts this turn.",
    });
    expect(env.posts).toHaveLength(2);
  });

  test("an order in flight at the turn's end isn't counted twice", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 10);
    expect(env.play.state.get().castles.get(1)?.unreserved).toBe(20);
    // The server took it in generation 10; generation 11 shows castle 1 after it.
    env.game.set(withCastle1(11, 21));
    expect(env.play.state.get().castles.get(1)?.unreserved).toBe(21);
    expect(env.play.send(1, 4, 15).ok).toBe(true);
  });

  test("the outcome with orders in flight and queued", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    env.play.send(1, 3, 5);
    env.play.upgrade(1, "speed");
    env.game.set(
      handState(11, {
        outcome: {
          reason: "last_team_standing",
          summary: "",
          winner_team_color: 2,
        },
      }),
    );
    expect(env.settled.map((order) => [order.id, order.sentence])).toEqual([
      [2, "The game has ended."],
      [3, "The game has ended."],
    ]);
    env.posts[0]?.reject(problem(409, "game-ended"));
    await env.clock.advance(5000);
    expect(env.posts).toHaveLength(1);
    expect(env.settled.at(-1)).toMatchObject({
      id: 1,
      sentence: "The game ended before the order arrived.",
    });
  });

  test("elimination stops a retry", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    env.posts[0]?.reject(transport());
    await flush();
    env.game.set(handState(11, { team_color: 5 }));
    await env.clock.advance(5000);
    expect(env.posts).toHaveLength(1);
    expect(env.settled).toEqual([
      expect.objectContaining({ sentence: "Your team is out of the game." }),
    ]);
  });

  test("dispose settles every order and makes no more requests", async () => {
    const env = setup({ initial: handState(10) });
    const first = env.play.send(1, 4, 5);
    const second = env.play.send(1, 3, 5);
    env.play.dispose();
    expect(first.ok && (await first.settled).state).toBe("rejected");
    expect(second.ok && (await second.settled).sentence).toBe(
      "You left the game.",
    );
    expect(env.posts[0]?.signal.aborted).toBe(true);
    await env.clock.advance(60_000);
    expect(env.posts).toHaveLength(1);
    expect(env.deadlines).toHaveLength(0);
  });

  test("leaving the game settles the orders", async () => {
    const env = setup({ initial: handState(10) });
    const first = env.play.send(1, 4, 5);
    env.game.set(null);
    expect(first.ok && (await first.settled).sentence).toBe(
      "You left the game.",
    );
    expect(env.play.state.get()).toMatchObject({ status: "idle", turn: null });
  });

  test("the orders of a turn leave the store when the next generation arrives", async () => {
    const env = setup({ initial: handState(10) });
    env.play.send(1, 4, 5);
    accept(env, 0);
    await flush();
    expect(env.play.state.get().orders).toHaveLength(1);
    env.game.set(handState(11));
    expect(env.play.state.get().orders).toHaveLength(0);
  });
});

describe("the turn clock", () => {
  test("calls at the turn's end, and asks again while the turn is open", async () => {
    // The state arrives 300 ms after the generation was created.
    const env = setup({ initial: handState(10, {}, 1_000_000 - 300) });
    expect(env.play.state.get().turn).toEqual({
      gameId: 7,
      generationId: 10,
      turnNumber: 10,
      paceMs: 1000,
      endsAt: 1_000_710,
      startsAt: 999_710,
    });
    await env.clock.advance(709);
    expect(env.deadlines).toHaveLength(0);
    await env.clock.advance(1);
    expect(env.deadlines).toHaveLength(1);
    const delays: number[] = [];
    for (let i = 0; i < 6; i++) {
      const before = env.clock.now();
      env.deadlines.at(-1)?.resolve(handState(10, {}, 1_000_000 - 300));
      const count = env.deadlines.length;
      while (env.deadlines.length === count) await env.clock.advance(1);
      delays.push(env.clock.now() - before);
    }
    expect(delays).toEqual([100, 200, 400, 800, 1000, 1000]);
    // The answer shows the next turn: it is installed, and its clock starts.
    env.deadlines.at(-1)?.resolve(handState(11, {}, env.clock.now()));
    await flush();
    expect(env.game.get()?.generation_id).toBe(11);
    expect(env.play.state.get().turn?.generationId).toBe(11);
    const count = env.deadlines.length;
    await env.clock.advance(999);
    expect(env.deadlines).toHaveLength(count);
    await env.clock.advance(1);
    expect(env.deadlines).toHaveLength(count + 1);
  });

  test("a deadline that looks far off is capped at the pace after the state arrived", async () => {
    // This tab's clock is 5 s behind the server's.
    const env = setup({ initial: handState(10, {}, 1_000_000 + 5000) });
    expect(env.play.state.get().turn?.endsAt).toBe(1_001_000);
    await env.clock.advance(1000);
    expect(env.deadlines).toHaveLength(1);
  });

  test("a turn already over is called at once", async () => {
    const env = setup({ initial: handState(10, {}, 1_000_000 - 5000) });
    await env.clock.advance(0);
    expect(env.deadlines).toHaveLength(1);
  });

  test("an unreadable deadline falls back to the pace", async () => {
    const env = setup({
      initial: handState(10, { turn_deadline_at: "soon" }),
    });
    expect(env.play.state.get().turn?.endsAt).toBe(1_001_000);
  });

  test("after one early answer, later turns are timed by the pace alone", async () => {
    // This tab's clock is 500 ms ahead: the deadline looks 500 ms closer than it is.
    const env = setup({ initial: handState(10, {}, 1_000_000 - 500) });
    await env.clock.advance(510);
    expect(env.deadlines).toHaveLength(1);
    env.deadlines[0]?.resolve(handState(10, {}, 1_000_000 - 500));
    await env.clock.advance(100);
    expect(env.deadlines).toHaveLength(2);
    // The next turn starts now; its deadline again looks 500 ms closer.
    const arrival = env.clock.now();
    env.deadlines[1]?.resolve(handState(11, {}, arrival - 500));
    await flush();
    expect(env.play.state.get().turn?.endsAt).toBe(arrival + 1000);
    await env.clock.advance(999);
    expect(env.deadlines).toHaveLength(2);
    await env.clock.advance(1);
    expect(env.deadlines).toHaveLength(3);
    env.deadlines[2]?.resolve(handState(12, {}, env.clock.now() - 500));
    await flush();
    await env.clock.advance(1000);
    // One call per turn.
    expect(env.deadlines).toHaveLength(4);
  });

  test("failures keep the ladder going; a call with no answer times out", async () => {
    const env = setup({ initial: handState(10) });
    await env.clock.advance(1000);
    env.deadlines[0]?.reject(transport());
    await env.clock.advance(100);
    expect(env.deadlines).toHaveLength(2);
    await env.clock.advance(2000);
    expect(env.deadlines[1]?.signal.aborted).toBe(true);
    await env.clock.advance(200);
    expect(env.deadlines).toHaveLength(3);
  });

  test("a game the server refuses to show: realtime reads it, and the calls go on slowly", async () => {
    const env = setup({ initial: handState(10) });
    await env.clock.advance(1000);
    env.deadlines[0]?.reject(new ApiError({ kind: "problem", status: 404 }));
    await flush();
    expect(env.log.refreshes).toBe(1);
    await env.clock.advance(4999);
    expect(env.deadlines).toHaveLength(1);
    await env.clock.advance(1);
    expect(env.deadlines).toHaveLength(2);
    env.deadlines[1]?.reject(new ApiError({ kind: "problem", status: 404 }));
    await env.clock.advance(5000);
    expect(env.log.refreshes).toBe(1);
    expect(env.deadlines).toHaveLength(3);
  });

  test("a new turn's call doesn't wait for the old turn's", async () => {
    const env = setup({ initial: handState(10) });
    await env.clock.advance(1000);
    expect(env.deadlines).toHaveLength(1);
    env.game.set(handState(11, {}, env.clock.now()));
    await env.clock.advance(1000);
    expect(env.deadlines).toHaveLength(2);
  });

  test("turns ended by someone else, with no request for 2 s, get one call", async () => {
    // Each turn is ended by another player 900 ms in, before this tab's timer.
    const env = setup({ initial: handState(10) });
    await flush();
    await env.clock.advance(900);
    env.game.set(handState(11, {}, env.clock.now()));
    await env.clock.advance(900);
    env.game.set(handState(12, {}, env.clock.now()));
    // The upgrade table's read, 1800 ms ago, was this tab's last request.
    expect(env.deadlines).toHaveLength(0);
    await env.clock.advance(900);
    env.game.set(handState(13, {}, env.clock.now()));
    expect(env.deadlines).toHaveLength(1);
    env.deadlines[0]?.resolve(handState(13, {}, env.clock.now()));
    await env.clock.advance(900);
    env.game.set(handState(14, {}, env.clock.now()));
    // No ladder for it, and the next one waits for another 2 s of silence.
    expect(env.deadlines).toHaveLength(1);
  });

  test("no call for an eliminated team, after the outcome, in a game that isn't real-time, or for a bot", async () => {
    const cases: Partial<GenerationState>[] = [
      { team_color: 5 },
      {
        outcome: {
          reason: "generation_cap",
          summary: "",
          winner_team_color: 1,
        },
        turn_deadline_at: null,
      },
      { turn_pace_milliseconds: null },
      { bot: { kind: "localhost" } },
    ];
    for (const extra of cases) {
      const env = setup({ initial: handState(10, extra) });
      await env.clock.advance(60_000);
      expect(env.deadlines).toHaveLength(0);
    }
  });

  test("a turn's end time stays as it was when a later state of the same turn arrives", async () => {
    const env = setup({ initial: handState(10) });
    await env.clock.advance(300);
    env.adopt({ ...handState(10), log_lines_stored: 1 });
    expect(env.play.state.get().turn?.endsAt).toBe(1_001_000);
  });

  test("the retry ladder", () => {
    expect([0, 1, 2, 3, 4, 5].map(retryDelay)).toEqual([
      100, 200, 400, 800, 1000, 1000,
    ]);
  });
});
