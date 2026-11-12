import { describe, expect, test } from "bun:test";
import type { Snapshot } from "../../realtime";

import {
  buildRoom,
  canChangeSettings,
  choiceFromValue,
  choiceValue,
  describeBot,
  describeSettings,
  memberSettingsBot,
  startBlocker,
  teamTaken,
} from "./roomModel";

const member = (
  id: string,
  teamId: string | null,
  extra: Record<string, unknown> = {},
) => ({
  id: `m-${id}`,
  player_id: id,
  display_name: id.toUpperCase(),
  active: true,
  ready: false,
  team_id: teamId,
  generation: 1,
  join_sequence: Number(id.slice(1)),
  joined_at: "2026-11-01T00:00:00Z",
  disconnected_until: null,
  ...extra,
});

// Each person's bot is in their member settings.
const localhost = { member_settings: { bot: { kind: "localhost" } } };
const levelTwo = { member_settings: { bot: { kind: "default", level: 2 } } };

const snapshot = {
  channel: "lobby:island-battles:L",
  membership_generation: 1,
  lobby: {
    id: "L",
    name: "Test",
    host_player_id: "p1",
    phase: "waiting",
    version: 3,
    game_settings: {},
  },
  teams: [
    { id: "t-red", name: "Red", order: 2 },
    { id: "t-blue", name: "Blue", order: 1 },
    { id: "t-green", name: "Green", order: 3 },
  ],
  members: [
    member("p2", "t-red", { ready: true, ...levelTwo }),
    member("p1", "t-blue", localhost),
    member("p3", null, {
      disconnected_until: "2026-11-01T00:01:00Z",
      member_settings: null,
    }),
    member("p4", "t-blue", { active: false }),
  ],
} as unknown as Snapshot;

describe("a bot in the member settings", () => {
  test("is read from {bot: ...}, and anything else is no bot", () => {
    expect(memberSettingsBot(localhost.member_settings)).toEqual({
      kind: "localhost",
    });
    expect(memberSettingsBot(levelTwo.member_settings)).toEqual({
      kind: "default",
      level: 2,
    });
    for (const settings of [
      null,
      undefined,
      {},
      { bot: null },
      { bot: { kind: "default" } },
      { bot: { kind: "uploaded" } },
    ]) {
      expect(memberSettingsBot(settings)).toBeUndefined();
    }
  });
});

describe("the room model", () => {
  const room = buildRoom(snapshot, "p1");

  test("teams in order with their colour, members and bots", () => {
    expect(room.teams.map((team) => [team.name, team.colour])).toEqual([
      ["Blue", 1],
      ["Red", 2],
      ["Green", 3],
    ]);
    expect(room.teams[0]?.members.map((m) => [m.name, m.bot?.kind])).toEqual([
      ["P1", "localhost"],
    ]);
    expect(room.teams[1]?.members[0]).toMatchObject({
      name: "P2",
      ready: true,
      bot: { kind: "default", level: 2 },
    });
  });

  test("members without a team, left members and disconnected members", () => {
    expect(room.unassigned.map((m) => [m.name, m.connected, m.bot])).toEqual([
      ["P3", false, undefined],
    ]);
    const names = [
      ...room.teams.flatMap((t) => t.members),
      ...room.unassigned,
    ].map((m) => m.name);
    expect(names).not.toContain("P4");
  });

  test("a team with a member is taken, an empty one is free", () => {
    expect(room.teams.map((team) => [team.name, teamTaken(team)])).toEqual([
      ["Blue", true],
      ["Red", true],
      ["Green", false],
    ]);
  });

  test("who you are", () => {
    expect(room.you?.name).toBe("P1");
    expect(room.isHost).toBe(true);
    expect(room.yourTeamId).toBe("t-blue");
    expect(buildRoom(snapshot, "p2").isHost).toBe(false);
  });
});

describe("a change of the map", () => {
  test("only the host of a waiting lobby can change it", () => {
    expect(canChangeSettings(buildRoom(snapshot, "p1"))).toBe(true);
    expect(canChangeSettings(buildRoom(snapshot, "p2"))).toBe(false);
    const started = {
      ...snapshot,
      lobby: { ...snapshot.lobby, phase: "started" },
    } as Snapshot;
    expect(canChangeSettings(buildRoom(started, "p1"))).toBe(false);
  });

  test("the teams, seats and bots follow the rebuilt teams", () => {
    // A two-team map: Green is gone, so its seat was removed and the person
    // who was on it has no team; the first two teams kept their IDs.
    const changed = {
      ...snapshot,
      lobby: {
        ...snapshot.lobby,
        version: 4,
        game_settings: { map_template_id: 4 },
      },
      teams: [
        { id: "t-blue", name: "Blue", order: 1 },
        { id: "t-red", name: "Red", order: 2 },
      ],
      members: [
        member("p1", "t-blue", localhost),
        member("p2", null, levelTwo),
        member("p5", null, {
          active: false,
          kind: "seat",
          seat_settings: { bot_level: 2 },
        }),
      ],
    } as unknown as Snapshot;
    const room = buildRoom(changed, "p1");
    expect(room.teams.map((team) => [team.id, team.colour])).toEqual([
      ["t-blue", 1],
      ["t-red", 2],
    ]);
    expect(room.teams[1]?.members).toEqual([]);
    expect(
      room.unassigned.map((row) => [row.name, row.ready, row.bot?.kind]),
    ).toEqual([["P2", false, "default"]]);
    expect(startBlocker(room, { every_team_needs_member: true })).toBe(
      "Everyone needs a team.",
    );
  });
});

describe("bot choices", () => {
  test("round-trip through the select's values", () => {
    for (const choice of [
      { kind: "localhost" as const },
      { kind: "default" as const, level: 3 },
    ]) {
      expect(choiceFromValue(choiceValue(choice))).toEqual(choice);
    }
    expect(choiceValue(undefined)).toBe("");
    expect(choiceFromValue("")).toBeNull();
    expect(choiceFromValue("level-0")).toBeNull();
  });

  test("words for a bot", () => {
    expect(describeBot(undefined)).toBe("No bot yet");
    expect(describeBot({ kind: "localhost" })).toBe("Own bot (localhost)");
    expect(describeBot({ kind: "default", level: 2 })).toBe(
      "Default bot, level 2",
    );
  });
});

describe("the settings line", () => {
  test("names the map and the turn limit", () => {
    const maps = [{ map_template_id: 4, map_name: "Strait" }] as never;
    expect(describeSettings({}, maps)).toBe(
      "Map: generated for the players · Turn limit: 30 s",
    );
    expect(
      describeSettings({ map_template_id: 4, turn_timeout_seconds: 120 }, maps),
    ).toBe("Map: Strait · Turn limit: 2 min");
    expect(
      describeSettings(
        { map_template_id: 5, turn_timeout_seconds: null },
        maps,
      ),
    ).toBe("Map: default map #5 · No turn limit");
    expect(describeSettings({ map: { map_name: "Mine" } }, maps)).toBe(
      "Map: Mine · Turn limit: 30 s",
    );
  });
});

describe("what keeps the host from starting", () => {
  const rules = { every_team_needs_member: false } as Snapshot["start_rules"];
  const strict = { every_team_needs_member: true } as Snapshot["start_rules"];
  const seat = (id: string, teamId: string) =>
    member(id, teamId, {
      kind: "seat",
      ready: true,
      seat_settings: { bot_level: 1 },
    });
  /** The room for these members, with the snapshot's teams (Blue, Red and Green). */
  const roomOf = (members: unknown[]) =>
    buildRoom({ ...snapshot, members } as unknown as Snapshot, "p1");

  test("fewer than two members, people and bot seats together", () => {
    expect(
      startBlocker(roomOf([member("p1", "t-blue", { ready: true })]), rules),
    ).toBe("Waiting for another player or bot.");
    expect(
      startBlocker(
        roomOf([member("p1", "t-blue", { ready: true }), seat("p2", "t-red")]),
        rules,
      ),
    ).toBeNull();
  });

  test("a member without a team", () => {
    expect(
      startBlocker(
        roomOf([
          member("p1", "t-blue", { ready: true }),
          member("p2", null, { ready: true }),
        ]),
        rules,
      ),
    ).toBe("Everyone needs a team.");
  });

  test("a person who is reconnecting", () => {
    expect(
      startBlocker(
        roomOf([
          member("p1", "t-blue", { ready: true }),
          member("p2", "t-red", {
            ready: true,
            disconnected_until: "2026-11-01T00:01:00Z",
          }),
        ]),
        rules,
      ),
    ).toBe("Waiting for P2 to reconnect.");
  });

  test("a person who is not ready; a bot seat is always ready", () => {
    expect(
      startBlocker(
        roomOf([
          member("p1", "t-blue", { ready: true }),
          member("p2", "t-red", { ready: false }),
        ]),
        rules,
      ),
    ).toBe("Waiting for P2 to be ready.");
    expect(
      startBlocker(
        roomOf([member("p1", "t-blue", { ready: true }), seat("p2", "t-red")]),
        rules,
      ),
    ).toBeNull();
  });

  test("an empty team, only when the game requires a member in each", () => {
    const members = [
      member("p1", "t-blue", { ready: true }),
      member("p2", "t-blue", { ready: true }),
    ];
    expect(startBlocker(roomOf(members), strict)).toBe(
      "Every team needs a player or a bot.",
    );
    expect(startBlocker(roomOf(members), rules)).toBeNull();
  });

  test("null when every rule is met", () => {
    expect(
      startBlocker(
        roomOf([
          member("p1", "t-blue", { ready: true }),
          member("p2", "t-red", { ready: true }),
          seat("p3", "t-green"),
        ]),
        strict,
      ),
    ).toBeNull();
  });
});
