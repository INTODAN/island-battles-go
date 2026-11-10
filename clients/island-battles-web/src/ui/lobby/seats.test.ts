import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import type { Realtime, Snapshot } from "../../realtime";
import { createLobbyActions, retryTransport } from "./actions";
import { describeProblem } from "./problems";
import { buildRoom, countPeople, seatBody, seatLevel } from "./roomModel";

const member = (
  id: string,
  teamId: string | null,
  extra: Record<string, unknown> = {},
) => ({
  id: `m-${id}`,
  player_id: id,
  display_name: id.toUpperCase(),
  kind: "human",
  active: true,
  ready: false,
  team_id: teamId,
  generation: 1,
  join_sequence: Number(id.replace(/\D/g, "")) || 1,
  joined_at: "2026-11-01T00:00:00Z",
  disconnected_until: null,
  seat_settings: null,
  ...extra,
});

const seat = (id: string, teamId: string, level: number, extra = {}) =>
  member(id, teamId, {
    kind: "seat",
    display_name: `Bot (level ${level})`,
    ready: true,
    seat_settings: { bot_level: level },
    ...extra,
  });

const snapshot = {
  channel: "lobby:island-battles:L",
  membership_generation: 1,
  lobby: {
    id: "L",
    game_key: "island-battles",
    name: "Test",
    host_player_id: "p1",
    phase: "waiting",
    version: 4,
    game_settings: {},
  },
  teams: [
    { id: "t-red", name: "Red", order: 2 },
    { id: "t-blue", name: "Blue", order: 1 },
    { id: "t-green", name: "Green", order: 3 },
  ],
  members: [
    member("p1", "t-blue", { ready: true }),
    seat("seat-1", "t-red", 2, { id: "s-1" }),
    member("p2", "t-green", {
      disconnected_until: "2026-11-01T00:01:00Z",
    }),
  ],
} as unknown as Snapshot;

describe("seats in the room model", () => {
  const room = buildRoom(snapshot, null, "p1");
  const red = room.teams.find((team) => team.name === "Red");

  test("a seat is the member of its team, shown as a bot", () => {
    expect(red?.members.map((m) => [m.name, m.kind])).toEqual([
      ["Bot (level 2)", "seat"],
    ]);
    expect(red?.members[0]).toMatchObject({
      memberId: "s-1",
      bot: { kind: "default", level: 2 },
      ready: true,
      connected: true,
      isHost: false,
      isYou: false,
    });
  });

  test("a seat never has a lease, even with a disconnect time", () => {
    const seatRow = buildRoom(
      {
        ...snapshot,
        members: [
          seat("seat-1", "t-red", 2, {
            disconnected_until: "2026-11-01T00:01:00Z",
          }),
        ],
      } as unknown as Snapshot,
      null,
      "p1",
    ).teams.find((team) => team.name === "Red")?.members[0];
    expect(seatRow?.connected).toBe(true);
  });

  test("people are counted without the seats", () => {
    expect(countPeople(room.teams.flatMap((team) => team.members))).toBe(2);
    expect(
      countPeople(
        buildRoom(
          {
            ...snapshot,
            members: [seat("seat-1", "t-red", 1)],
          } as unknown as Snapshot,
          null,
          "p1",
        ).teams.flatMap((team) => team.members),
      ),
    ).toBe(0);
  });

  test("seat levels come from the settings", () => {
    expect(seatLevel({ bot_level: 3 })).toBe(3);
    expect(seatLevel({ bot_level: "3" })).toBeUndefined();
    expect(seatLevel(null)).toBeUndefined();
  });
});

describe("the add-seat request", () => {
  test("carries the team, the level and the request ID", () => {
    expect(seatBody("t-red", 2, "req-1")).toEqual({
      request_id: "req-1",
      team_id: "t-red",
      seat_settings: { bot_level: 2 },
    });
  });
});

describe("seat problems", () => {
  const seatError = (code: string) =>
    new ApiError({ kind: "problem", status: 409, code, detail: "server" });

  test("each seat problem is a sentence", () => {
    expect(describeProblem(seatError("not_host"), "seat").message).toBe(
      "Only the host can add or remove bots.",
    );
    expect(describeProblem(seatError("team_full"), "seat").message).toBe(
      "That team already has a player or a bot.",
    );
    expect(describeProblem(seatError("phase_conflict"), "seat").message).toBe(
      "The lobby has already started, so its bots can't change.",
    );
    expect(
      describeProblem(
        new ApiError({
          kind: "problem",
          status: 422,
          code: "invalid_seat_settings",
          detail: "bot_level: unknown",
        }),
        "seat",
      ).message,
    ).toBe("That bot level was refused. Choose another level.");
    expect(
      describeProblem(seatError("idempotency_conflict"), "seat").message,
    ).toBe("The lobby changed while that bot was being added. Try again.");
    expect(describeProblem(seatError("lobby_full"), "seat").message).toBe(
      "This lobby is full. Every seat is taken.",
    );
  });
});

/** Actions over a fake realtime and a fake fetch that records each request. */
function actionsWith(
  respond: (request: Request, body: unknown) => Response | Promise<Response>,
) {
  const requests: { method: string; path: string; body: unknown }[] = [];
  const adopted: Snapshot[] = [];
  const realtime = {
    session: { get: () => ({ accessToken: "tok", playerId: "p1" }) },
    lobby: { get: () => snapshot },
    adoptSnapshot: (next: Snapshot) => adopted.push(next),
  } as unknown as Realtime;
  // The API clients take their base URL from the page's location when they are made.
  const actions = withLocation("http://api.test", () =>
    createLobbyActions({
      realtime,
      clientInstanceId: "client-1",
      fetch: (async (input: Request | string | URL, init?: RequestInit) => {
        const request = new Request(input, init);
        const body = request.body ? await request.json() : undefined;
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body,
        });
        return respond(request, body);
      }) as typeof fetch,
    }),
  );
  const ok = () =>
    new Response(JSON.stringify(snapshot), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  return { actions, requests, adopted, ok };
}

function withLocation<T>(origin: string, make: () => T): T {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "location");
  Object.defineProperty(globalThis, "location", {
    value: { origin },
    configurable: true,
  });
  try {
    return make();
  } finally {
    if (descriptor) {
      Object.defineProperty(globalThis, "location", descriptor);
    } else {
      Reflect.deleteProperty(globalThis, "location");
    }
  }
}

describe("the add and remove calls", () => {
  test("adding posts the body and adopts the snapshot it returns", async () => {
    const { actions, requests, adopted, ok } = actionsWith(() => ok());
    await actions.addSeat(seatBody("t-red", 2, "req-1"));
    expect(requests).toEqual([
      {
        method: "POST",
        path: "/lobby/v1/games/island-battles/lobbies/L/seats",
        body: {
          request_id: "req-1",
          team_id: "t-red",
          seat_settings: { bot_level: 2 },
        },
      },
    ]);
    expect(adopted).toHaveLength(1);
  });

  test("removing deletes the seat by its ID and adopts the snapshot", async () => {
    const { actions, requests, adopted, ok } = actionsWith(() => ok());
    await actions.removeSeat("s-1");
    expect(requests).toEqual([
      {
        method: "DELETE",
        path: "/lobby/v1/games/island-battles/lobbies/L/seats/s-1",
        body: undefined,
      },
    ]);
    expect(adopted).toHaveLength(1);
  });

  test("a transport retry resends the same request ID, so one seat is added", async () => {
    let attempts = 0;
    const { actions, requests, ok } = actionsWith(() => {
      attempts++;
      if (attempts === 1) throw new TypeError("connection reset");
      return ok();
    });
    const body = seatBody("t-red", 1, "req-9");
    await retryTransport(() => actions.addSeat(body), {
      delayMs: 0,
      sleep: async () => undefined,
    });
    expect(
      requests.map((r) => (r.body as { request_id: string }).request_id),
    ).toEqual(["req-9", "req-9"]);
  });
});
