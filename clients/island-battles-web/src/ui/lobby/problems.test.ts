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

  test("reopening explains a running game and a lobby that can't reopen", () => {
    expect(describeProblem(lobby("game_running"), "reopen").message).toBe(
      "The game is still running. Its lobby opens once it ends.",
    );
    expect(describeProblem(lobby("phase_conflict"), "reopen").message).toBe(
      "This lobby can't be opened again.",
    );
  });

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
    expect(shown.message).toMatch(/^The settings were refused/);
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
      "Choose your bot first: play by hand, your localhost bot or a default level.",
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

  test("a lobby with its maximum of watchers", () => {
    const full = describeProblem(
      new ApiError({
        kind: "problem",
        status: 409,
        code: "watchers_full",
      }),
      "watch",
    );
    expect(full.message).toBe(
      "This game has as many watchers as it allows. Try again later.",
    );
    expect(full.lostMembership).toBe(false);
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

describe("a rated lobby's refusals", () => {
  test("the ready gate's sentence is shown as the server gives it", () => {
    for (const [type, detail] of [
      [
        "rated-sign-in-required",
        "This lobby is rated. Sign in with Google in the 3D client to play in it.",
      ],
      [
        "rated-bot-name-required",
        "This lobby is rated. Play by hand, or choose one of your hosted bot names.",
      ],
      [
        "rated-hosted-bot-required",
        "This lobby is rated. Rated bots run at their own URL: choose one of your hosted bot names, or play by hand.",
      ],
      [
        "bot-name-url-required",
        "Your bot name Kraken has no URL yet. Add its hosted bot's URL on the account page, then get ready again.",
      ],
      [
        "rated-bot-name-gone",
        "Your bot name Kraken no longer exists. Choose another.",
      ],
      [
        "rated-same-account",
        "Dana and Dana are the same account. A rated game needs a different account on each team.",
      ],
      [
        "rated-bot-not-listed",
        "Kraken is no longer on the leaderboard. Choose another bot.",
      ],
      [
        "rated-bot-paused",
        "Kraken is paused after it missed a turn, until its owner resumes it. Choose another bot.",
      ],
      [
        "rated-pair-limit",
        "Kraken and Bot (level 2) have played their rated game with Kraken on Red today. Swap their teams, or play again tomorrow (UTC).",
      ],
      [
        "rated-bot-day-limit",
        "Kraken has played its 10 rated games away from its owner today. Play it again tomorrow (UTC).",
      ],
      [
        "rated-seat",
        "A rated game allows only default bot seats, each level once, and bots from the leaderboard, each once. Remove Hosted bot.",
      ],
    ] as const) {
      const error = lobby("game_requirement", {
        problemType: `urn:island-battles:problem:${type}`,
        detail,
      });
      expect(describeProblem(error, "ready").message).toBe(detail);
      expect(describeProblem(error, "start").message).toBe(detail);
    }
  });

  test("a refused seat shows the rule, and a refused name the game's sentence", () => {
    const seat = lobby("invalid_seat_settings", {
      status: 422,
      detail:
        "A rated game allows only default bot seats, each level once, and bots from the leaderboard, each once. This lobby already has Bot level 2.",
    });
    expect(describeProblem(seat, "seat").message).toBe(
      "A rated game allows only default bot seats, each level once, and bots from the leaderboard, each once. This lobby already has Bot level 2.",
    );
    const name = lobby("invalid_member_settings", {
      status: 422,
      detail: "Octopus isn't one of your bot names.",
    });
    expect(describeProblem(name, "bot").message).toBe(
      "Octopus isn't one of your bot names.",
    );
    const noUrl = lobby("invalid_member_settings", {
      status: 422,
      detail:
        "Kraken has no URL yet: add its hosted bot's URL on the account page, or give your hosted bot a url.",
    });
    expect(describeProblem(noUrl, "bot").message).toBe(noUrl.detail ?? "");
  });

  test("a refused bot from the leaderboard in a seat shows the game's sentence", () => {
    const paused = lobby("invalid_seat_settings", {
      status: 422,
      detail:
        "Kraken is paused after it missed a turn, until its owner resumes it. Choose another bot.",
    });
    expect(describeProblem(paused, "listed-seat").message).toBe(
      paused.detail ?? "",
    );
    // Another seat's refusal keeps the room's own sentence.
    expect(describeProblem(paused, "seat").message).toBe(
      "That bot level was refused. Choose another level.",
    );
    expect(describeProblem(lobby("not_host"), "listed-seat").message).toBe(
      "Only the host can add or remove bots.",
    );
  });

  test("turning Rated on with a seat it doesn't allow says which", () => {
    expect(
      describeProblem(
        lobby("invalid_game_settings", {
          status: 422,
          detail:
            "A rated game allows only default bot seats, each level once, and bots from the leaderboard, each once. Remove Hosted bot.",
        }),
        "settings",
      ).message,
    ).toBe(
      "The settings were refused. A rated game allows only default bot seats, each level once, and bots from the leaderboard, each once. Remove Hosted bot.",
    );
  });
});
