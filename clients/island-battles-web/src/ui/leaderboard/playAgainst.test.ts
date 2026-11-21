import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import type { Snapshot } from "../../realtime";
import type { AddSeatBody, MapTemplateSummary } from "../lobby/actions";
import type { CreateLobbyBody } from "../lobby/form";
import {
  noHostedBotsProblem,
  noMapProblem,
  type PlayAgainstActions,
  playAgainstLobbyName,
  playAgainstMap,
  ratedSignInProblem,
  signInToPlayNotice,
  startPlayAgainst,
} from "./playAgainst";

const map = (id: number, teams: number): MapTemplateSummary => ({
  map_template_id: id,
  map_name: `Map ${id}`,
  team_count: teams,
  castle_count: 10,
  plane_size_x: 100,
  plane_size_y: 100,
});

const kraken = {
  name: "Kraken",
  competitorId: "c-kraken",
  url: "https://kraken.example.org/bot",
  rated: false,
};
const you = "p-you";

/** A fake lobby: records the calls, and answers a snapshot of two teams. */
function fakeLobby(
  options: {
    serverBots?: boolean;
    maps?: MapTemplateSummary[];
    onTeam?: string | null;
    seatError?: ApiError;
    createError?: ApiError;
    rated?: boolean;
    signedIn?: boolean;
  } = {},
) {
  const calls: string[] = [];
  const created: Omit<CreateLobbyBody, "client_instance_id" | "game_key">[] =
    [];
  const seats: AddSeatBody[] = [];
  const teams: string[] = [];
  const actions: PlayAgainstActions = {
    async botOptions() {
      calls.push("botOptions");
      return { bots: [], serverBots: options.serverBots ?? true };
    },
    async mapTemplates() {
      calls.push("mapTemplates");
      return options.maps ?? [map(1, 4), map(2, 2)];
    },
    async create(body) {
      calls.push("create");
      if (options.createError) throw options.createError;
      created.push(body);
      return {
        lobby: { id: "l1" },
        // Out of order: the first team is the one with order 0.
        teams: [
          { id: "t-red", name: "Red", order: 1 },
          { id: "t-blue", name: "Blue", order: 0 },
        ],
        members: [
          {
            player_id: you,
            active: true,
            team_id: options.onTeam === undefined ? null : options.onTeam,
          },
        ],
      } as unknown as Snapshot;
    },
    async selectTeam(teamId) {
      calls.push("selectTeam");
      teams.push(teamId ?? "");
    },
    async addSeat(body) {
      calls.push("addSeat");
      if (options.seatError) throw options.seatError;
      seats.push(body);
    },
  };
  let id = 0;
  return {
    actions,
    calls,
    created,
    seats,
    teams,
    play: () =>
      startPlayAgainst(
        { ...kraken, rated: options.rated ?? false },
        {
          actions,
          playerId: you,
          signedIn: options.signedIn ?? true,
          newId: () => `id-${++id}`,
        },
      ),
  };
}

const rejection = async (promise: Promise<unknown>): Promise<Error> => {
  try {
    await promise;
  } catch (error) {
    return error as Error;
  }
  throw new Error("expected a rejection");
};

describe("Play against it", () => {
  test("makes an unrated lobby with you on one team and the bot as a seat on another", async () => {
    const lobby = fakeLobby();
    expect(await lobby.play()).toEqual({});
    expect(lobby.calls).toEqual([
      "botOptions",
      "mapTemplates",
      "create",
      "selectTeam",
      "addSeat",
    ]);
    // No rated setting: the lobby is unrated, so the game changes no rating.
    expect(lobby.created).toEqual([
      {
        name: "Against Kraken",
        teams: [{ name: "Blue" }, { name: "Red" }],
        game_settings: {
          map_template_id: 2,
          turn_timeout_seconds: 30,
          turn_pace_milliseconds: 1000,
          max_generations: 300,
        },
        request_id: "id-1",
      },
    ]);
    expect(lobby.teams).toEqual(["t-blue"]);
    // The seat names the bot by its competitor ID; the server plays it at its name's URL.
    expect(lobby.seats).toEqual([
      {
        request_id: "id-2",
        team_id: "t-red",
        seat_settings: { bot_competitor_id: "c-kraken" },
      },
    ]);
  });

  test("makes a rated lobby, with the same seat, for a signed-in viewer", async () => {
    const lobby = fakeLobby({ rated: true });
    expect(await lobby.play()).toEqual({});
    expect(lobby.created[0]?.game_settings).toEqual({
      map_template_id: 2,
      turn_timeout_seconds: 30,
      turn_pace_milliseconds: 1000,
      max_generations: 300,
      rated: true,
    });
    expect(lobby.seats[0]?.seat_settings).toEqual({
      bot_competitor_id: "c-kraken",
    });
  });

  test("a guest can't host a rated lobby, and is asked to sign in first", async () => {
    const lobby = fakeLobby({ rated: true, signedIn: false });
    expect((await rejection(lobby.play())).message).toBe(
      ratedSignInProblem("Kraken"),
    );
    expect(ratedSignInProblem("Kraken")).toBe(
      "Sign in with Google to play a rated game against Kraken, or play an unrated one.",
    );
    expect(lobby.calls).toEqual([]);
    // An unrated lobby needs no sign-in.
    expect(await fakeLobby({ signedIn: false }).play()).toEqual({});
    expect(signInToPlayNotice("Kraken")).toBe(
      "Choose a name to play against Kraken. A lobby with Kraken as a bot opens next.",
    );
    expect(signInToPlayNotice("Kraken", true)).toBe(
      "Sign in with Google to play a rated game against Kraken. A rated lobby with Kraken as a bot opens next.",
    );
  });

  test("keeps the team the lobby gave you, and seats the bot on another", async () => {
    const lobby = fakeLobby({ onTeam: "t-red" });
    await lobby.play();
    expect(lobby.calls).not.toContain("selectTeam");
    expect(lobby.seats[0]?.team_id).toBe("t-blue");
  });

  test("plays on a map for two teams, else the first a lobby can have", () => {
    expect(
      playAgainstMap([map(1, 4), map(2, 2), map(3, 2)])?.map_template_id,
    ).toBe(2);
    expect(playAgainstMap([map(1, 1), map(4, 3)])?.map_template_id).toBe(4);
    expect(playAgainstMap([map(1, 1), map(5, 7)])).toBeUndefined();
    expect(playAgainstLobbyName("Kraken")).toBe("Against Kraken");
    expect([...playAgainstLobbyName("x".repeat(60))]).toHaveLength(40);
  });

  test("makes no lobby when the server plays no hosted bots, or has no map", async () => {
    const noBots = fakeLobby({ serverBots: false });
    expect((await rejection(noBots.play())).message).toBe(
      noHostedBotsProblem("Kraken"),
    );
    expect(noHostedBotsProblem("Kraken")).toBe(
      "This server doesn't play hosted bots, so you can't play against Kraken here.",
    );
    expect(noBots.calls).not.toContain("create");
    const noMaps = fakeLobby({ maps: [map(1, 1)] });
    expect((await rejection(noMaps.play())).message).toBe(noMapProblem);
    expect(noMaps.calls).not.toContain("create");
  });

  test("a refused create rejects with the lobby's sentence", async () => {
    const lobby = fakeLobby({
      createError: new ApiError({ kind: "transport", status: 0 }),
    });
    expect((await rejection(lobby.play())).message).toStartWith(
      "Cannot reach the server.",
    );
  });

  test("a refused seat leaves the lobby, and says why and how to add it", async () => {
    const lobby = fakeLobby({
      rated: true,
      seatError: new ApiError({
        kind: "problem",
        status: 422,
        code: "invalid_seat_settings",
        detail:
          "Kraken is paused after it missed a turn, until its owner resumes it. Choose another bot.",
      }),
    });
    expect(await lobby.play()).toEqual({
      notice:
        "Kraken wasn't added: Kraken is paused after it missed a turn, until its owner resumes it. Choose another bot. Add it with Add bot, as a bot from the leaderboard.",
    });
  });
});
