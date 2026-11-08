import { describe, expect, test } from "bun:test";
import type { Snapshot } from "../../realtime";
import type { LobbyBots } from "./actions";
import {
  buildRoom,
  choiceFromValue,
  choiceValue,
  describeBot,
  describeSettings,
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
    { id: "t-red", name: "Red", order: 2, capacity: 1 },
    { id: "t-blue", name: "Blue", order: 1, capacity: 2 },
  ],
  members: [
    member("p2", "t-red", { ready: true }),
    member("p1", "t-blue"),
    member("p3", null, { disconnected_until: "2026-11-01T00:01:00Z" }),
    member("p4", "t-blue", { active: false }),
  ],
} as unknown as Snapshot;

const bots: LobbyBots = {
  lobby_id: "L",
  members: [
    { player_id: "p1", display_name: "P1", bot: { kind: "localhost" } },
    { player_id: "p2", display_name: "P2", bot: { kind: "default", level: 2 } },
  ],
};

describe("the room model", () => {
  const room = buildRoom(snapshot, bots, "p1");

  test("teams in order with their colour, members and bots", () => {
    expect(room.teams.map((team) => [team.name, team.colour])).toEqual([
      ["Blue", 1],
      ["Red", 2],
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

  test("who you are", () => {
    expect(room.you?.name).toBe("P1");
    expect(room.isHost).toBe(true);
    expect(room.yourTeamId).toBe("t-blue");
    expect(buildRoom(snapshot, null, "p2").isHost).toBe(false);
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
