import { describe, expect, test } from "bun:test";
import { ApiError } from "../api/errors";
import type { Snapshot } from "../realtime";
import { createStore } from "../realtime/store";
import {
  buildRoom,
  choiceFromValue,
  describeBot,
  localhostSeats,
  memberSettingsBot,
  seatSettingsFromValue,
  startBlocker,
  yourPorts,
  yourSeats,
} from "../ui/lobby/roomModel";
import {
  BotError,
  checkBotHealth,
  deliverTurn,
  type GenerationState,
  nextFreePort,
  parseBotPort,
  portProblem,
  serveCommandFor,
  type TurnDelivery,
} from "./localhost";
import { createBotRelay } from "./relay";

const delivery = { turn: { game_id: 1 } } as unknown as TurnDelivery;

const recording = (urls: string[], status = 202) =>
  (async (url: string) => {
    urls.push(url);
    return new Response("", { status });
  }) as unknown as typeof fetch;

describe("a localhost bot's port", () => {
  test("gets its turns and health checks on its own port", async () => {
    const urls: string[] = [];
    await deliverTurn(delivery, { port: 41415, fetch: recording(urls) });
    await deliverTurn(delivery, { fetch: recording(urls) });
    expect(
      await checkBotHealth({ port: 41416, fetch: recording(urls, 200) }),
    ).toBe(true);
    expect(urls).toEqual([
      "http://127.0.0.1:41415/turn",
      "http://127.0.0.1:41414/turn",
      "http://127.0.0.1:41416/health",
    ]);
  });

  test("names its port when the bot is not running", async () => {
    const error = await deliverTurn(delivery, {
      port: 41415,
      fetch: (async () => {
        throw new TypeError("Failed to fetch");
      }) as unknown as typeof fetch,
    }).catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(BotError);
    expect((error as BotError).message).toBe(
      "Your bot is not reachable on 127.0.0.1:41415. Start it with island-battles-bot serve --port 41415.",
    );
  });

  test("is a whole number from 1024 to 65535, free among your localhost bots", () => {
    expect(parseBotPort("41415")).toBe(41415);
    expect(parseBotPort(" 1024 ")).toBe(1024);
    expect(parseBotPort(65535)).toBe(65535);
    for (const invalid of [
      "",
      "80",
      "1023",
      "65536",
      "41 415",
      "4141.5",
      "x",
    ]) {
      expect(parseBotPort(invalid)).toBeNull();
    }
    expect(portProblem("41415", [41414])).toBeNull();
    expect(portProblem("41414", [41414])).toBe(
      "Port 41414 is taken by another of your localhost bots: give each its own port.",
    );
    expect(portProblem("80")).toMatch(/from 1024 to 65535/);
    expect(nextFreePort([41414, 41415, 41417])).toBe(41416);
    expect(serveCommandFor(41414)).toBe(
      "uv run island-battles-bot serve bot:play",
    );
    expect(serveCommandFor(41415)).toBe(
      "uv run island-battles-bot serve --port 41415 bot:play",
    );
  });
});

const member = (
  id: string,
  teamId: string,
  extra: Record<string, unknown> = {},
) => ({
  id: `m-${id}`,
  player_id: id,
  display_name: id,
  active: true,
  ready: true,
  team_id: teamId,
  generation: 1,
  join_sequence: Number(id.replace(/\D/g, "") || 0),
  joined_at: "2026-11-01T00:00:00Z",
  disconnected_until: null,
  kind: "human",
  ...extra,
});

const seat = (
  id: string,
  teamId: string,
  port: number | string,
  relay = "p1",
) =>
  member(id, teamId, {
    kind: "seat",
    display_name: `Localhost bot (port ${port})`,
    seat_settings: { bot_localhost_port: port, bot_relay_player_id: relay },
  });

const lobby = {
  channel: "lobby:island-battles:L",
  membership_generation: 1,
  lobby: {
    id: "L",
    name: "Ports",
    host_player_id: "p1",
    phase: "waiting",
    version: 1,
    game_settings: {},
  },
  teams: [
    { id: "t1", name: "Red", order: 1 },
    { id: "t2", name: "Blue", order: 2 },
    { id: "t3", name: "Green", order: 3 },
    { id: "t4", name: "Gold", order: 4 },
    { id: "t5", name: "Teal", order: 5 },
  ],
  members: [
    member("p1", "t1", {
      member_settings: { bot: { kind: "localhost", port: 41420 } },
    }),
    seat("s2", "t2", "41415"),
    seat("s3", "t3", 41416),
    member("p4", "t4", {
      member_settings: { bot: { kind: "localhost", port: 41415 } },
    }),
    // Added by a host who left since: nobody relays it.
    seat("s5", "t5", 41417, "p9"),
  ],
} as unknown as Snapshot;

describe("localhost bots in the room", () => {
  test("a person's port and a seat's, which the lobby page may send as digits", () => {
    expect(
      memberSettingsBot({ bot: { kind: "localhost", port: 41415 } }),
    ).toEqual({ kind: "localhost", port: 41415 });
    expect(memberSettingsBot({ bot: { kind: "localhost" } })).toEqual({
      kind: "localhost",
    });
    const model = buildRoom(lobby, "p1");
    expect(model.teams.map((team) => team.members[0]?.bot)).toEqual([
      { kind: "localhost", port: 41420 },
      { kind: "localhost", port: 41415 },
      { kind: "localhost", port: 41416 },
      { kind: "localhost", port: 41415 },
      { kind: "localhost", port: 41417 },
    ]);
    // The host relays their own bot and the seats they added; another person's port is on their
    // machine, and a seat whose adder left is nobody's.
    const p1 = { playerId: "p1", name: "p1", isYou: true };
    expect(yourPorts(model)).toEqual([41420, 41415, 41416]);
    expect(localhostSeats(model)).toEqual([
      { name: "Localhost bot (port 41415)", port: 41415, relay: p1 },
      { name: "Localhost bot (port 41416)", port: 41416, relay: p1 },
      { name: "Localhost bot (port 41417)", port: 41417, relay: null },
    ]);
    expect(yourSeats(model).map((seat) => seat.port)).toEqual([41415, 41416]);
    expect(model.teams[4]?.members[0]?.relay).toBeNull();
    // Only its adder may relay it, so the start waits for the host to remove it.
    expect(startBlocker(model, { every_team_needs_member: true })).toBe(
      "Remove Localhost bot (port 41417): the player who added it left. The host can add it again from their own tab.",
    );
  });

  test("a player relays only the seats they added, whoever the host is", () => {
    // p4 is now the host; p1 still relays the seats that p1 added.
    const model = buildRoom(
      { ...lobby, lobby: { ...lobby.lobby, host_player_id: "p4" } },
      "p4",
    );
    expect(model.isHost).toBe(true);
    expect(yourPorts(model)).toEqual([41415]);
    expect(yourSeats(model)).toEqual([]);
    expect(model.teams[1]?.members[0]?.relay).toEqual({
      playerId: "p1",
      name: "p1",
      isYou: false,
    });
  });

  test("words and choices for a port", () => {
    expect(describeBot({ kind: "localhost", port: 41415 })).toBe(
      "Own bot (localhost:41415)",
    );
    expect(describeBot({ kind: "localhost", port: 41414 })).toBe(
      "Own bot (localhost)",
    );
    const relay = { playerId: "p1", name: "Alex", isYou: false };
    expect(describeBot({ kind: "localhost", port: 41416 }, relay)).toBe(
      "Localhost bot on Alex's port 41416",
    );
    expect(
      describeBot(
        { kind: "localhost", port: 41416 },
        { ...relay, isYou: true },
      ),
    ).toBe("Localhost bot on your port 41416");
    expect(describeBot({ kind: "localhost", port: 41416 }, null)).toBe(
      "Localhost bot on port 41416, whoever added it left, so the start is refused until it is removed",
    );
    expect(choiceFromValue("localhost", "", 41415)).toEqual({
      kind: "localhost",
      port: 41415,
    });
    expect(choiceFromValue("localhost", "", 41414)).toEqual({
      kind: "localhost",
    });
    expect(seatSettingsFromValue("localhost", "", " 41417 ", "p1")).toEqual({
      bot_localhost_port: 41417,
      bot_relay_player_id: "p1",
    });
    expect(seatSettingsFromValue("localhost", "", "80", "p1")).toBeNull();
    // Without the host's player ID there's nobody to relay it.
    expect(seatSettingsFromValue("localhost", "", "41417")).toBeNull();
  });
});

function gameState(
  generationId: number,
  extra: Partial<GenerationState> = {},
): GenerationState {
  return {
    bot: { kind: "default", level: 1 },
    relayed_bots: [],
    game_id: 7,
    generation_id: generationId,
    generation: { id: generationId },
    submitted: true,
    turn_deadline_at: new Date(0).toISOString(),
    ...extra,
  } as unknown as GenerationState;
}

const flush = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve();
};

describe("a seat's relay", () => {
  test("delivers each turn whatever the host's own bot, and leaves the deadline to the host's relay", async () => {
    const game = createStore<GenerationState | null>(null);
    const delivered: number[] = [];
    let deadlines = 0;
    let reads = 0;
    const relay = createBotRelay({
      game,
      seat: true,
      async loadDelivery() {
        reads++;
        const generationId = game.get()?.generation_id ?? 0;
        if (generationId === 3) {
          throw new ApiError({
            kind: "problem",
            status: 409,
            detail: "player turn is already complete",
          });
        }
        return {
          turn: { generation: { id: generationId } },
        } as unknown as TurnDelivery;
      },
      async deliver(sent) {
        delivered.push(sent.turn.generation.id);
      },
      async applyDeadline() {
        deadlines++;
      },
      now: () => Date.now(),
      schedule(run, ms) {
        const timer = setTimeout(run, ms);
        return () => clearTimeout(timer);
      },
    });
    game.set(gameState(1));
    await flush();
    game.set(gameState(2));
    await flush();
    expect(delivered).toEqual([1, 2]);
    expect(relay.status.get()).toEqual({ kind: "delivered", generationId: 2 });
    // Nothing to play: the seat waits for the next turn.
    game.set(gameState(3));
    await flush();
    expect(reads).toBe(3);
    expect(relay.status.get()).toEqual({ kind: "delivered", generationId: 3 });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(deadlines).toBe(0);
    relay.dispose();
  });
});
