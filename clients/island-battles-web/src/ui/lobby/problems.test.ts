import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import { describeProblem } from "./problems";

const lobby = (
  code: string,
  extra: Partial<ConstructorParameters<typeof ApiError>[0]> = {},
) =>
  new ApiError({
    kind: "problem",
    status: 409,
    code,
    detail: "server words",
    ...extra,
  });

describe("problem sentences", () => {
  const sentences: [string, string][] = [
    ["lobby_full", "This lobby is full. Every seat is taken."],
    ["team_full", "That team is taken. Please choose another team."],
    ["phase_conflict", "The lobby has already started or closed."],
    [
      "not_ready",
      "Every player needs a team and must be ready before starting.",
    ],
    ["empty_teams", "Every team needs a player before the game can start."],
    ["not_host", "Only the host can start the lobby."],
  ];
  for (const [code, sentence] of sentences) {
    test(`${code} shows its sentence`, () => {
      expect(describeProblem(lobby(code), "start").message).toBe(sentence);
    });
  }

  test("empty_teams highlights the teams that need a player", () => {
    const shown = describeProblem(
      lobby("empty_teams", { teamIds: ["t2", "t3"] }),
      "start",
    );
    expect(shown.teamIds).toEqual(["t2", "t3"]);
    expect(describeProblem(lobby("not_ready"), "start").teamIds).toEqual([]);
  });

  test("invalid_game_settings gives the server's detail about the map", () => {
    const shown = describeProblem(
      lobby("invalid_game_settings", {
        detail: "game_settings.map.castles[0].x: must be inside the plane",
      }),
      "create",
    );
    expect(shown.message).toContain("game_settings.map.castles[0].x");
    expect(shown.message).toMatch(/^The map or turn limit was refused/);
  });

  test("stale_membership means the player is no longer in the lobby", () => {
    const shown = describeProblem(lobby("stale_membership"), "ready");
    expect(shown.lostMembership).toBe(true);
    expect(shown.message).toBe(
      "You're no longer connected to that lobby. Use its code to join again.",
    );
  });

  test("a code that matches no lobby is a sentence on join, not a lost membership", () => {
    const shown = describeProblem(lobby("not_found", { status: 404 }), "join");
    expect(shown.lostMembership).toBe(false);
    expect(shown.message).toBe("No lobby found for that code.");
    expect(
      describeProblem(lobby("not_found", { status: 404 }), "ready")
        .lostMembership,
    ).toBe(true);
  });

  test("bot-not-chosen, as the lobby's game_requirement", () => {
    const error = lobby("game_requirement", {
      problemType: "urn:island-battles:problem:bot-not-chosen",
      detail: "Choose your bot before getting ready.",
    });
    expect(describeProblem(error, "ready").message).toBe(
      "Choose your bot first: your localhost bot or a default level.",
    );
    // At the start the server names who is missing a bot.
    const atStart = lobby("game_requirement", {
      problemType: "urn:island-battles:problem:bot-not-chosen",
      detail: "Sam has not chosen a bot.",
    });
    expect(describeProblem(atStart, "start").message).toBe(
      "Sam has not chosen a bot.",
    );
  });

  test("session and network problems", () => {
    const expired = describeProblem(
      new ApiError({ kind: "problem", status: 401, code: "unauthenticated" }),
      "ready",
    );
    expect(expired.signedOut).toBe(true);
    const offline = describeProblem(
      new ApiError({ kind: "transport", status: 0 }),
      "ready",
    );
    expect(offline.message).toContain("Cannot reach the server");
  });
});
