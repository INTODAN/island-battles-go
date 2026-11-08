import { describe, expect, test } from "bun:test";
import {
  type CreateFormValues,
  CreateRequests,
  describeTurnTimeout,
  fixedTeamCount,
  type MapTemplate,
  type MapTemplateSummary,
  normalizeJoinCode,
  readMapFile,
  teamName,
  validateCreateForm,
  validateDisplayName,
  validateJoinCode,
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
  teamCount: 2,
  teamCapacity: 1,
  map: { kind: "generated" },
  turnTimeoutSeconds: 30,
  ...extra,
});

describe("the create form", () => {
  test("a generated map takes the chosen teams, named after the board's colours", () => {
    const result = validateCreateForm(
      values({ teamCount: 3, teamCapacity: 2 }),
      templates,
    );
    expect(result).toEqual({
      ok: true,
      teams: [
        { name: "Blue", capacity: 2 },
        { name: "Red", capacity: 2 },
        { name: "Green", capacity: 2 },
      ],
      capacity: 6,
      gameSettings: { turn_timeout_seconds: 30 },
    });
  });

  test("a template or a file sets the teams, whatever the team count field says", () => {
    const template = validateCreateForm(
      values({ teamCount: 6, map: { kind: "template", templateId: 4 } }),
      templates,
    );
    expect(template).toMatchObject({
      ok: true,
      capacity: 3,
      gameSettings: { map_template_id: 4, turn_timeout_seconds: 30 },
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
      capacity: 4,
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
        teamCount: 7,
        teamCapacity: 0,
        turnTimeoutSeconds: 7,
      }),
      templates,
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(Object.keys(result.errors).sort()).toEqual([
        "lobbyName",
        "teamCapacity",
        "teamCount",
        "turnTimeout",
      ]);
    }
  });

  test("allows 2 to 6 teams only", () => {
    for (const teamCount of [1, 7, 2.5, Number.NaN]) {
      expect(validateCreateForm(values({ teamCount }), templates).ok).toBe(
        false,
      );
    }
    for (const teamCount of [2, 3, 4, 5, 6]) {
      expect(validateCreateForm(values({ teamCount }), templates).ok).toBe(
        true,
      );
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
    expect(fixedTeamCount({ kind: "generated" }, templates)).toBeNull();
    expect(fixedTeamCount({ kind: "template", templateId: 4 }, templates)).toBe(
      3,
    );
    expect(fixedTeamCount({ kind: "file", map: null }, templates)).toBeNull();
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
    const first = requests.idFor({ name: "A", capacity: 2 });
    // The create did not get an answer, so the same body goes out again.
    expect(requests.idFor({ name: "A", capacity: 2 })).toBe(first);
    // A different lobby is a different create.
    expect(requests.idFor({ name: "A", capacity: 4 })).not.toBe(first);
  });
});
