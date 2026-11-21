import { describe, expect, test } from "bun:test";
import type { Snapshot } from "../../realtime";

import {
  anyPlaysByHand,
  botNameUrlBlocker,
  buildRoom,
  canChangeSettings,
  choiceFromValue,
  choiceValue,
  describeBot,
  describeRealTime,
  describeSettings,
  guestTag,
  memberSettingsBot,
  ratedBlockers,
  ratedBotNameBlocker,
  ratedHostedBlocker,
  ratedSignInBlocker,
  seatLevels,
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
    expect(
      memberSettingsBot({
        bot: { kind: "hosted", url: "https://bot.example.com/turn" },
      }),
    ).toEqual({ kind: "hosted", url: "https://bot.example.com/turn" });
    expect(
      memberSettingsBot({ bot: { kind: "uploaded", package_id: "pkg-1" } }),
    ).toEqual({ kind: "uploaded", package_id: "pkg-1" });
    expect(memberSettingsBot({ bot: { kind: "manual" } })).toEqual({
      kind: "manual",
    });
    for (const settings of [
      null,
      undefined,
      {},
      { bot: null },
      { bot: { kind: "default" } },
      { bot: { kind: "uploaded" } },
      { bot: { kind: "hosted" } },
      { bot: { kind: "remote" } },
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
      { kind: "uploaded" as const, package_id: "pkg-1" },
    ]) {
      expect(choiceFromValue(choiceValue(choice))).toEqual(choice);
    }
    expect(choiceValue(undefined)).toBe("");
    expect(choiceFromValue("")).toBeNull();
    expect(choiceFromValue("level-0")).toBeNull();
    expect(choiceFromValue("package-")).toBeNull();
  });

  test("a hosted bot takes the URL typed next to it", () => {
    const hosted = {
      kind: "hosted" as const,
      url: "https://bot.example.com/turn",
    };
    expect(choiceValue(hosted)).toBe("hosted");
    expect(choiceFromValue("hosted", " https://bot.example.com/turn ")).toEqual(
      hosted,
    );
    expect(choiceFromValue("hosted", "")).toBeNull();
  });

  test("words for a bot", () => {
    expect(describeBot(undefined)).toBe("No bot yet");
    expect(describeBot({ kind: "localhost" })).toBe("Own bot (localhost)");
    expect(describeBot({ kind: "default", level: 2 })).toBe(
      "Default bot, level 2",
    );
    expect(
      describeBot({ kind: "hosted", url: "https://bot.example.com/a/turn" }),
    ).toBe("Hosted bot (bot.example.com)");
    expect(describeBot({ kind: "uploaded", package_id: "pkg-1" })).toBe(
      "Uploaded bot",
    );
    expect(describeBot({ kind: "manual" })).toBe("Plays by hand");
  });

  test("a manual choice is named in the room and is the select's current value", () => {
    const manual = {
      ...snapshot,
      members: (snapshot.members ?? []).map((entry) =>
        entry.player_id === "p1"
          ? { ...entry, member_settings: { bot: { kind: "manual" } } }
          : entry,
      ),
    } as unknown as Snapshot;
    const you = buildRoom(manual, "p1").you;
    expect(you?.bot).toEqual({ kind: "manual" });
    expect(describeBot(you?.bot)).toBe("Plays by hand");
    expect(choiceValue(you?.bot)).toBe("manual");
  });
});

describe("playing by hand in the room", () => {
  const hand = { member_settings: { bot: { kind: "manual" } } };
  const roomOf = (members: unknown[]) =>
    buildRoom({ ...snapshot, members } as unknown as Snapshot, "p1");

  test("the picker's value maps to the manual choice and back", () => {
    expect(choiceFromValue("manual")).toEqual({ kind: "manual" });
    expect(choiceValue(choiceFromValue("manual") ?? undefined)).toBe("manual");
  });

  test("a member who plays by hand shows it, and the summary is real-time", () => {
    const room = roomOf([
      member("p1", "t-blue", hand),
      member("p2", "t-red", levelTwo),
    ]);
    expect(describeBot(room.you?.bot)).toBe("Plays by hand");
    expect(anyPlaysByHand(room)).toBe(true);
    expect(
      describeSettings(
        { turn_pace_milliseconds: 500 },
        [],
        anyPlaysByHand(room),
      ),
    ).toBe("No map chosen · Real time: a turn every 0.5 s");
  });

  test("any member counts, even one who has no team yet", () => {
    const room = roomOf([
      member("p1", "t-blue", levelTwo),
      member("p2", null, hand),
    ]);
    expect(anyPlaysByHand(room)).toBe(true);
  });

  test("without a hand player the summary keeps the turn limit", () => {
    const room = roomOf([
      member("p1", "t-blue", levelTwo),
      member("p2", "t-red", localhost),
    ]);
    expect(anyPlaysByHand(room)).toBe(false);
    expect(describeSettings({}, [], anyPlaysByHand(room))).toBe(
      "No map chosen · Turn limit: 30 s",
    );
  });

  test("the pace defaults to 1 s and shows seconds", () => {
    expect(describeRealTime({})).toBe("Real time: a turn every 1 s");
    expect(describeRealTime({ turn_pace_milliseconds: 2000 })).toBe(
      "Real time: a turn every 2 s",
    );
  });
});

describe("the settings line", () => {
  test("names the map and the turn limit", () => {
    const maps = [{ map_template_id: 4, map_name: "Strait" }] as never;
    expect(describeSettings({}, maps)).toBe("No map chosen · Turn limit: 30 s");
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

  test("says when the lobby is rated", () => {
    expect(describeSettings({ map_template_id: 5, rated: true }, [])).toBe(
      "Map: default map #5 · Turn limit: 30 s · Rated",
    );
    expect(describeSettings({ map_template_id: 5, rated: false }, [])).toBe(
      "Map: default map #5 · Turn limit: 30 s",
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

test("a room names the game length when it isn't 300 turns", () => {
  expect(
    describeSettings({ map_template_id: 4, max_generations: 300 }, []),
  ).toBe("Map: default map #4 · Turn limit: 30 s");
  expect(
    describeSettings({ map_template_id: 4, max_generations: 600 }, []),
  ).toBe("Map: default map #4 · Turn limit: 30 s · Game length: 600 turns");
});

describe("a rated lobby", () => {
  const signedIn = (extra: Record<string, unknown> = {}) => ({
    signed_in: true,
    ...extra,
  });
  const named = (kind: string, name?: string, more = {}) => ({
    member_settings: { bot: { kind, ...more, ...(name ? { name } : {}) } },
  });
  const seatOf = (id: string, teamId: string | null, settings: object) =>
    member(id, teamId, { kind: "seat", ready: true, seat_settings: settings });
  /** A room for these members, seen by p1, who is the host; rated unless said otherwise. */
  const roomOf = (members: unknown[], rated = true, you = "p1") =>
    buildRoom(
      {
        ...snapshot,
        lobby: { ...snapshot.lobby, game_settings: { rated } },
        members,
      } as unknown as Snapshot,
      you,
    );
  const kraken = {
    botNames: [
      { name: "Kraken", url: "https://kraken.example.org/bot" },
      { name: "Squid", url: "https://squid.example.org/" },
      { name: "Old", url: null },
    ],
  };

  test("reads rated from the settings, and who signed in from the snapshot", () => {
    const room = roomOf([
      member("p1", "t-blue", signedIn(named("manual"))),
      member("p2", "t-red"),
    ]);
    expect(room.rated).toBe(true);
    expect(room.you?.signedIn).toBe(true);
    expect(room.teams[1]?.members[0]?.signedIn).toBe(false);
    expect(roomOf([member("p1", "t-blue")], false).rated).toBe(false);
  });

  test("a lobby that isn't rated blocks nothing", () => {
    expect(
      ratedBlockers(
        roomOf(
          [
            member("p1", "t-blue", named("default", undefined, { level: 2 })),
            member("p2", "t-red"),
            seatOf("p3", "t-green", { bot_url: "https://bot.example.com" }),
          ],
          false,
        ),
        null,
      ),
    ).toEqual({ you: null, host: null, guests: [] });
  });

  test("a guest must sign in, and the others see them tagged guest", () => {
    const guest = roomOf(
      [
        member("p1", "t-blue", signedIn()),
        member("p2", "t-red", named("manual")),
      ],
      true,
      "p2",
    );
    expect(ratedBlockers(guest, null).you).toBe(ratedSignInBlocker);
    expect(ratedSignInBlocker).toBe("Sign in to play in this rated lobby.");
    // The host sees the guest's tag; nobody is tagged for themself.
    const host = roomOf([
      member("p1", "t-blue", signedIn(named("manual"))),
      member("p2", "t-red", named("manual")),
    ]);
    expect(ratedBlockers(host, kraken)).toEqual({
      you: null,
      host: null,
      guests: ["m-p2"],
    });
    expect(ratedBlockers(guest, null).guests).toEqual([]);
    expect(guestTag).toBe("guest");
  });

  test("a bot needs one of your hosted bot names, and a default bot never plays for you", () => {
    const you = (settings: Record<string, unknown>) =>
      ratedBlockers(
        roomOf([member("p1", "t-blue", signedIn(settings))]),
        kraken,
      ).you;
    expect(you(named("default", undefined, { level: 2 }))).toBe(
      ratedBotNameBlocker,
    );
    expect(ratedBotNameBlocker).toBe(
      "Choose one of your hosted bot names, or play by hand.",
    );
    expect(you(named("localhost"))).toBe(ratedBotNameBlocker);
    expect(you(named("hosted", undefined, { url: "https://b.example" }))).toBe(
      ratedBotNameBlocker,
    );
    expect(you(named("uploaded", undefined, { package_id: "pk" }))).toBe(
      ratedBotNameBlocker,
    );
    expect(you(named("hosted", "Kraken"))).toBeNull();
    // Names compare without regard to case, as the server compares them.
    expect(you(named("hosted", "squid"))).toBeNull();
    // A url beside the name is ignored in a rated lobby, as the server ignores it.
    expect(
      you(named("hosted", "Kraken", { url: "https://other.example" })),
    ).toBeNull();
    expect(you(named("manual"))).toBeNull();
    // Without a bot, the server's "choose your bot first" applies, as in any lobby.
    expect(you({})).toBeNull();
  });

  test("a named localhost or uploaded bot is refused: rated bots run at their own URL", () => {
    const you = (settings: Record<string, unknown>) =>
      ratedBlockers(
        roomOf([member("p1", "t-blue", signedIn(settings))]),
        kraken,
      ).you;
    expect(you(named("localhost", "Kraken"))).toBe(ratedHostedBlocker);
    expect(you(named("uploaded", "Kraken", { package_id: "pk" }))).toBe(
      ratedHostedBlocker,
    );
    expect(ratedHostedBlocker).toBe(
      "Rated bots run at their own URL: choose one of your hosted bot names, or play by hand.",
    );
  });

  test("a name without a URL needs one, on the account page", () => {
    const room = roomOf([
      member("p1", "t-blue", signedIn(named("hosted", "old"))),
    ]);
    expect(ratedBlockers(room, kraken).you).toBe(botNameUrlBlocker("Old"));
    expect(botNameUrlBlocker("Old")).toBe(
      "Your bot name Old has no URL yet. Add its hosted bot's URL on the account page.",
    );
    expect(ratedBlockers(room, null).you).toBeNull();
  });

  test("a deleted bot name is named, once your names are known", () => {
    const room = roomOf([
      member("p1", "t-blue", signedIn(named("hosted", "Octopus"))),
    ]);
    expect(ratedBlockers(room, kraken).you).toBe(
      "Your bot name Octopus no longer exists. Choose another.",
    );
    // Until the names are read, the server decides.
    expect(ratedBlockers(room, null).you).toBeNull();
  });

  test("a named hosted bot without a URL is read from the settings, and shown by its name", () => {
    const rated = roomOf([
      member(
        "p1",
        "t-blue",
        signedIn(named("hosted", "Kraken", { url: "https://typed.example" })),
      ),
    ]);
    // In a rated lobby the name's URL plays, so the typed one is dropped.
    expect(rated.you?.bot).toEqual({ kind: "hosted", name: "Kraken" });
    expect(describeBot(rated.you?.bot)).toBe("Kraken (hosted bot)");
    const unrated = roomOf(
      [member("p1", "t-blue", signedIn(named("hosted", "Kraken")))],
      false,
    );
    expect(unrated.you?.bot).toEqual({ kind: "hosted", name: "Kraken" });
    const typed = roomOf(
      [
        member(
          "p1",
          "t-blue",
          signedIn(named("hosted", "Kraken", { url: "https://typed.example" })),
        ),
      ],
      false,
    );
    expect(describeBot(typed.you?.bot)).toBe(
      "Kraken (hosted bot, typed.example)",
    );
  });

  test("the host is told which seats to remove, in the server's terms", () => {
    const members = [
      member("p1", "t-blue", signedIn(named("manual"))),
      seatOf("p2", "t-red", { bot_level: 2 }),
      seatOf("p3", "t-green", { bot_level: 2 }),
    ];
    expect(ratedBlockers(roomOf(members), kraken).host).toBe(
      "Remove P3: a rated game allows only default bot seats, each level once.",
    );
    const hosted = [
      member("p1", "t-blue", signedIn(named("manual"))),
      seatOf("p2", "t-red", { bot_url: "https://bot.example.com" }),
      seatOf("p3", "t-green", { bot_level: 1 }),
    ];
    expect(ratedBlockers(roomOf(hosted), kraken).host).toBe(
      "Remove P2: a rated game allows only default bot seats, each level once.",
    );
    // Only the host is told.
    expect(ratedBlockers(roomOf(members, true, "p2"), null).host).toBeNull();
    expect(
      ratedBlockers(
        roomOf([
          member("p1", "t-blue", signedIn(named("manual"))),
          seatOf("p2", "t-red", { bot_level: 2 }),
        ]),
        kraken,
      ),
    ).toEqual({ you: null, host: null, guests: [] });
  });

  test("the seats' levels, for Add bot", () => {
    expect(
      seatLevels(
        roomOf([
          seatOf("p2", "t-red", { bot_level: 3 }),
          seatOf("p3", "t-green", { bot_level: 1 }),
        ]),
      ),
    ).toEqual([3, 1]);
    // A seat on no team counts too, as the server counts it.
    expect(
      seatLevels(
        roomOf([
          seatOf("p2", "t-red", { bot_level: 3 }),
          seatOf("p3", null, { bot_level: 2 }),
        ]),
      ),
    ).toEqual([3, 2]);
  });
});

describe("a bot that plays under a bot name", () => {
  test("is read from the member settings with its name", () => {
    expect(
      memberSettingsBot({ bot: { kind: "localhost", name: "Kraken" } }),
    ).toEqual({ kind: "localhost", name: "Kraken" });
    expect(
      memberSettingsBot({
        bot: { kind: "uploaded", package_id: "pk", name: "Squid" },
      }),
    ).toEqual({ kind: "uploaded", package_id: "pk", name: "Squid" });
    // Play by hand and default bots have no name.
    expect(memberSettingsBot({ bot: { kind: "manual", name: "X" } })).toEqual({
      kind: "manual",
    });
  });

  test("is named in the room by its bot name", () => {
    expect(describeBot({ kind: "localhost", name: "Kraken" })).toBe(
      "Kraken (own bot, localhost)",
    );
    expect(
      describeBot({ kind: "localhost", port: 41415, name: "Kraken" }),
    ).toBe("Kraken (own bot, localhost:41415)");
    expect(
      describeBot({
        kind: "hosted",
        url: "https://bot.example.com/turn",
        name: "Kraken",
      }),
    ).toBe("Kraken (hosted bot, bot.example.com)");
    expect(
      describeBot({ kind: "uploaded", package_id: "pk", name: "Squid" }),
    ).toBe("Squid (uploaded bot)");
    expect(describeBot({ kind: "localhost" })).toBe("Own bot (localhost)");
  });

  test("the picker's value carries the bot name, and no name leaves it out", () => {
    expect(choiceFromValue("localhost", "", 41414, "Kraken")).toEqual({
      kind: "localhost",
      name: "Kraken",
    });
    expect(choiceFromValue("localhost", "", 41415, "Kraken")).toEqual({
      kind: "localhost",
      port: 41415,
      name: "Kraken",
    });
    expect(
      choiceFromValue(
        "hosted",
        " https://bot.example.com/turn ",
        41414,
        "Kraken",
      ),
    ).toEqual({
      kind: "hosted",
      url: "https://bot.example.com/turn",
      name: "Kraken",
    });
    expect(choiceFromValue("package-pk", "", 41414, "Squid")).toEqual({
      kind: "uploaded",
      package_id: "pk",
      name: "Squid",
    });
    expect(choiceFromValue("localhost", "", 41414, "")).toEqual({
      kind: "localhost",
    });
    // The name doesn't change which radio the choice is.
    expect(choiceValue({ kind: "localhost", name: "Kraken" })).toBe(
      "localhost",
    );
  });
});
