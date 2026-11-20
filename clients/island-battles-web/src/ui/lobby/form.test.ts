import { describe, expect, test } from "bun:test";
import {
  type CreateFormValues,
  CreateRequests,
  changeSettingsBody,
  describeTurnTimeout,
  lobbyMapSettings,
  type MapTemplate,
  type MapTemplateSummary,
  mapTeamCount,
  normalizeJoinCode,
  readMapFile,
  teamName,
  validateCreateForm,
  validateDisplayName,
  validateJoinCode,
  validateMapSettings,
} from "./form";

const templates: MapTemplateSummary[] = [
  {
    map_template_id: 4,
    map_name: "Strait",
    team_count: 3,
    castle_count: 9,
    plane_size_x: 10,
    plane_size_y: 8,
  },
  {
    map_template_id: 9,
    map_name: "Giant",
    team_count: 20,
    castle_count: 40,
    plane_size_x: 30,
    plane_size_y: 30,
  },
];

const values = (extra: Partial<CreateFormValues> = {}): CreateFormValues => ({
  lobbyName: "Alex's lobby",
  map: { kind: "template", templateId: 4 },
  turnTimeoutSeconds: 30,
  turnPaceMilliseconds: 1000,
  maxGenerations: 300,
  ...extra,
});

describe("the create form", () => {
  test("a default map or a file sets the teams, named after the board's colours", () => {
    // One player per team: the request names the teams and nothing else.
    expect(validateCreateForm(values(), templates)).toEqual({
      ok: true,
      teams: [{ name: "Blue" }, { name: "Red" }, { name: "Green" }],
      gameSettings: {
        map_template_id: 4,
        turn_timeout_seconds: 30,
        turn_pace_milliseconds: 1000,
        max_generations: 300,
      },
    });

    const map = {
      map_name: "Mine",
      player_amount: 4,
    } as unknown as MapTemplate;
    const file = validateCreateForm(
      values({ map: { kind: "file", map } }),
      templates,
    );
    expect(file).toMatchObject({
      ok: true,
      gameSettings: { map, turn_timeout_seconds: 30 },
    });
    if (file.ok)
      expect(file.teams.map((team) => team.name)).toEqual([
        "Blue",
        "Red",
        "Green",
        "Purple",
      ]);
  });

  test("needs a map", () => {
    expect(
      validateCreateForm(values({ map: { kind: "none" } }), templates),
    ).toEqual({
      ok: false,
      errors: { map: "Pick a default map, or load a map file." },
    });
  });

  test("sends null for no turn limit", () => {
    const result = validateCreateForm(
      values({ turnTimeoutSeconds: null }),
      templates,
    );
    expect(result).toMatchObject({
      ok: true,
      gameSettings: { turn_timeout_seconds: null },
    });
  });

  test("names each field that is wrong", () => {
    const result = validateCreateForm(
      values({
        lobbyName: "  ",
        map: { kind: "none" },
        turnTimeoutSeconds: 7,
      }),
      templates,
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(Object.keys(result.errors).sort()).toEqual([
        "lobbyName",
        "map",
        "turnTimeout",
      ]);
    }
  });

  test("refuses a map for more teams than a lobby allows", () => {
    const result = validateCreateForm(
      values({ map: { kind: "template", templateId: 9 } }),
      templates,
    );
    expect(result).toMatchObject({
      ok: false,
      errors: { map: expect.stringContaining("20 teams") },
    });
  });

  test("refuses a template that is gone and a file that is missing or was refused", () => {
    expect(
      validateCreateForm(
        values({ map: { kind: "template", templateId: 99 } }),
        templates,
      ),
    ).toMatchObject({
      ok: false,
      errors: { map: expect.any(String) },
    });
    expect(
      validateCreateForm(
        values({ map: { kind: "file", map: null } }),
        templates,
      ),
    ).toMatchObject({
      ok: false,
      errors: { map: "Choose a map file, or pick another map." },
    });
    expect(
      validateCreateForm(
        values({
          map: {
            kind: "file",
            map: null,
            error: "castles[2].x is outside the plane",
          },
        }),
        templates,
      ),
    ).toMatchObject({
      ok: false,
      errors: { map: "castles[2].x is outside the plane" },
    });
  });

  test("a map decides the team count", () => {
    expect(mapTeamCount({ kind: "none" }, templates)).toBeNull();
    expect(mapTeamCount({ kind: "template", templateId: 4 }, templates)).toBe(
      3,
    );
    expect(
      mapTeamCount({ kind: "template", templateId: 99 }, templates),
    ).toBeNull();
    expect(mapTeamCount({ kind: "file", map: null }, templates)).toBeNull();
  });

  test("reads a map file far enough for the form", () => {
    expect(readMapFile('{"map_name":"A","player_amount":2}').map_name).toBe(
      "A",
    );
    expect(() => readMapFile("{")).toThrow("not valid JSON");
    expect(() => readMapFile("[]")).toThrow("one map object");
    expect(() => readMapFile('{"map_name":"A"}')).toThrow("player_amount");
    expect(() => readMapFile('{"player_amount":2,"map_name":" "}')).toThrow(
      "map_name",
    );
  });

  test("describes a lobby's turn limit", () => {
    expect(describeTurnTimeout({})).toBe("Turn limit: 30 s");
    expect(describeTurnTimeout({ turn_timeout_seconds: 120 })).toBe(
      "Turn limit: 2 min",
    );
    expect(describeTurnTimeout({ turn_timeout_seconds: 10 })).toBe(
      "Turn limit: 10 s",
    );
    expect(describeTurnTimeout({ turn_timeout_seconds: null })).toBe(
      "No turn limit",
    );
  });

  test("names teams after the board's colours", () => {
    expect([1, 2, 3, 4, 5, 6, 7].map(teamName)).toEqual([
      "Blue",
      "Red",
      "Green",
      "Purple",
      "Amber",
      "Teal",
      "Team 7",
    ]);
  });
});

describe("the room's Change map form", () => {
  test("starts from the lobby's map and turn limit", () => {
    // Settings without a map start on the first default map once it loads.
    expect(lobbyMapSettings({})).toEqual({
      map: { kind: "none" },
      turnTimeoutSeconds: 30,
      currentTurnTimeoutSeconds: 30,
      turnPaceMilliseconds: 1000,
      currentTurnPaceMilliseconds: 1000,
      maxGenerations: 300,
      currentMaxGenerations: 300,
      rated: false,
    });
    expect(
      lobbyMapSettings({ map_template_id: 4, turn_timeout_seconds: null }),
    ).toMatchObject({
      map: { kind: "template", templateId: 4 },
      turnTimeoutSeconds: null,
    });
    const map = { map_name: "Mine", player_amount: 4 };
    expect(lobbyMapSettings({ map, turn_timeout_seconds: 120 })).toEqual({
      map: { kind: "file", map: map as unknown as MapTemplate },
      turnTimeoutSeconds: 120,
      currentTurnTimeoutSeconds: 120,
      turnPaceMilliseconds: 1000,
      currentTurnPaceMilliseconds: 1000,
      maxGenerations: 300,
      currentMaxGenerations: 300,
      rated: false,
    });
  });

  test("builds the request body: the map's teams and the settings", () => {
    const result = validateMapSettings(
      {
        map: { kind: "template", templateId: 4 },
        turnTimeoutSeconds: 600,
        turnPaceMilliseconds: 2000,
        maxGenerations: 600,
      },
      templates,
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(changeSettingsBody(result)).toEqual({
        teams: [{ name: "Blue" }, { name: "Red" }, { name: "Green" }],
        game_settings: {
          map_template_id: 4,
          turn_timeout_seconds: 600,
          turn_pace_milliseconds: 2000,
          max_generations: 600,
        },
      });
    }
  });

  test("keeps the lobby's rated setting when the host changes the map", () => {
    const values = lobbyMapSettings({ map_template_id: 4, rated: true });
    expect(values.rated).toBe(true);
    const result = validateMapSettings(
      { ...values, map: { kind: "template", templateId: 4 } },
      templates,
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(changeSettingsBody(result).game_settings).toMatchObject({
        map_template_id: 4,
        rated: true,
      });
    }
    const unrated = validateMapSettings(
      lobbyMapSettings({ map_template_id: 4 }),
      templates,
    );
    expect(unrated.ok && "rated" in unrated.gameSettings).toBe(false);
  });

  test("keeps a turn limit the list lacks only when it is the lobby's own", () => {
    const values = lobbyMapSettings({
      map_template_id: 4,
      turn_timeout_seconds: 45,
    });
    expect(validateMapSettings(values, templates)).toMatchObject({
      ok: true,
      gameSettings: { turn_timeout_seconds: 45 },
    });
    expect(
      validateMapSettings({ ...values, turnTimeoutSeconds: 46 }, templates),
    ).toMatchObject({ ok: false, errors: { turnTimeout: expect.any(String) } });
  });

  test("keeps a turn pace the list lacks only when it is the lobby's own", () => {
    const values = lobbyMapSettings({
      map_template_id: 4,
      turn_pace_milliseconds: 1500,
    });
    expect(values.turnPaceMilliseconds).toBe(1500);
    expect(validateMapSettings(values, templates)).toMatchObject({
      ok: true,
      gameSettings: { turn_pace_milliseconds: 1500 },
    });
    expect(
      validateMapSettings({ ...values, turnPaceMilliseconds: 700 }, templates),
    ).toMatchObject({ ok: false, errors: { turnPace: expect.any(String) } });
  });

  test("refuses a lobby's default map that is no longer listed", () => {
    const result = validateMapSettings(
      lobbyMapSettings({ map_template_id: 77 }),
      templates,
    );
    expect(result).toMatchObject({
      ok: false,
      errors: { map: expect.any(String) },
    });
  });
});

describe("codes and names", () => {
  test("a join code is trimmed and upper-cased, and has 8 characters", () => {
    expect(normalizeJoinCode("  abcd2345 ")).toBe("ABCD2345");
    expect(validateJoinCode(" abcd2345 ")).toBeUndefined();
    expect(validateJoinCode("")).toBe("Enter the lobby's join code.");
    expect(validateJoinCode("ABC")).toBe(
      "A join code has 8 letters and digits.",
    );
  });

  test("a name is needed and at most 40 characters", () => {
    expect(validateDisplayName("Alex")).toBeUndefined();
    expect(validateDisplayName("   ")).toBe("Enter your name first.");
    expect(validateDisplayName("x".repeat(41))).toContain("40");
  });
});

describe("create requests", () => {
  test("a fresh request id for each create", () => {
    let count = 0;
    const requests = new CreateRequests(() => `id-${++count}`);
    const first = requests.idFor({ name: "A" });
    requests.settled();
    expect(requests.idFor({ name: "A" })).not.toBe(first);
  });

  test("the same id is resent after a transport failure, until the body changes", () => {
    let count = 0;
    const requests = new CreateRequests(() => `id-${++count}`);
    const first = requests.idFor({ name: "A", teams: 2 });
    // The create did not get an answer, so the same body goes out again.
    expect(requests.idFor({ name: "A", teams: 2 })).toBe(first);
    // A different lobby is a different create.
    expect(requests.idFor({ name: "A", teams: 4 })).not.toBe(first);
  });
});

describe("the game length", () => {
  test("is sent, and the room's form starts from the lobby's own", () => {
    expect(
      validateCreateForm(values({ maxGenerations: 1200 }), templates),
    ).toMatchObject({ ok: true, gameSettings: { max_generations: 1200 } });
    expect(lobbyMapSettings({ map_template_id: 4 }).maxGenerations).toBe(300);
    const current = lobbyMapSettings({
      map_template_id: 4,
      max_generations: 50,
    });
    expect(current.maxGenerations).toBe(50);
    // A length the list lacks is accepted only as the lobby's own.
    expect(validateMapSettings(current, templates)).toMatchObject({
      ok: true,
      gameSettings: { max_generations: 50 },
    });
    expect(
      validateMapSettings({ ...current, maxGenerations: 51 }, templates),
    ).toMatchObject({ ok: false, errors: { gameLength: expect.any(String) } });
  });
});
