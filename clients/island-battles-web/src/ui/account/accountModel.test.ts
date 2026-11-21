import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import {
  accountProblem,
  accountView,
  botNameProblem,
  botUrlEditHint,
  botUrlPublicSentence,
  type Competitor,
  deleteAccountConfirmation,
  deleteBotNameConfirmation,
  displayNameProblem,
  endsSession,
  gamesText,
  type IslandAccount,
  noBotUrlSentence,
  ratingText,
  serverSentence,
  validateAccountName,
  validateBotName,
  validateBotUrl,
  withBotName,
  withChangedBotName,
  withoutBotName,
} from "./accountModel";

const competitor = (
  name: string,
  rating: number,
  games: number,
  kind: Competitor["kind"] = "bot",
): Competitor => ({
  id: `id-${name}`,
  kind,
  name,
  rating,
  games,
  wins: 0,
  draws: 0,
  losses: 0,
  last_played_at: null,
  url: kind === "bot" ? `https://bot.example.com/${name}` : null,
});

const account = (bots: Competitor[]): IslandAccount => ({
  account_id: "33333333-3333-4333-8333-333333333333",
  display_name: "Dana",
  max_bot_names: 5,
  hand: competitor("Dana", 1546.4, 18, "manual"),
  bot_names: bots,
});

const three = [
  competitor("Kraken", 1608.6, 52),
  competitor("Squid", 1488.2, 7),
  competitor("Narwhal", 1500, 0),
];
const five = [
  ...three,
  competitor("Sea Serpent", 1534, 19),
  competitor("Barnacle_2", 1471, 1),
];

const problem = (init: Partial<ConstructorParameters<typeof ApiError>[0]>) =>
  new ApiError({ kind: "problem", status: 409, ...init });

describe("names", () => {
  test("an account's display name is 3 to 24 letters, digits, spaces, - and _", () => {
    expect(validateAccountName("Dana")).toBeUndefined();
    expect(validateAccountName("  Dana B-2_x  ")).toBeUndefined();
    expect(validateAccountName("Åsa")).toBeUndefined();
    expect(validateAccountName("")).toBe("Enter a display name first.");
    expect(validateAccountName("Al")).toBe(
      "Use 3 to 24 letters, digits, spaces, - and _.",
    );
    expect(validateAccountName("x".repeat(25))).toContain("3 to 24");
    expect(validateAccountName("Dana!")).toContain("3 to 24");
  });

  test("a bot name can't start like a house bot", () => {
    expect(validateBotName("Kraken")).toBeUndefined();
    expect(validateBotName("Bot (level 2)")).toContain("3 to 24");
    expect(validateBotName("Bot level")).toBeUndefined();
    expect(validateBotName("")).toBe("Enter a bot name first.");
  });
});

describe("the account's view", () => {
  test("shows each competitor's rating rounded and its games", () => {
    const view = accountView(account(three));
    expect(view.hand).toEqual({
      id: "id-Dana",
      name: "Dana",
      rating: "1546",
      games: "18 rated games",
      url: null,
    });
    expect(view.bots.map((row) => [row.name, row.rating, row.games])).toEqual([
      ["Kraken", "1609", "52 rated games"],
      ["Squid", "1488", "7 rated games"],
      ["Narwhal", "1500", "no rated games yet"],
    ]);
    expect(view.count).toBe("3 of 5");
    expect(gamesText(1)).toBe("1 rated game");
    expect(ratingText(1499.5)).toBe("1500");
  });

  test("below the limit, a name can be added, with the rule and the count", () => {
    expect(accountView(account(three)).add).toEqual({
      disabled: false,
      limit: undefined,
      hint: "3 to 24 letters, digits, spaces, - and _. You have 3 of 5.",
    });
    expect(accountView(account(three), true).add.disabled).toBe(true);
  });

  test("at 5 names, the field and Add are disabled and say so", () => {
    const view = accountView(account(five));
    expect(view.count).toBe("5 of 5");
    expect(view.add).toEqual({
      disabled: true,
      limit: "You have 5 bot names, the most an account can have.",
      hint: "Delete one to register another.",
    });
  });

  test("adding and deleting a name changes the list and the limit", () => {
    const four = withBotName(account(three), competitor("Orca", 1500, 0));
    expect(accountView(four).count).toBe("4 of 5");
    const full = withBotName(four, competitor("Eel", 1500, 0));
    expect(accountView(full).add.disabled).toBe(true);
    const again = withoutBotName(full, "id-Squid");
    expect(accountView(again).bots.map((row) => row.name)).toEqual([
      "Kraken",
      "Narwhal",
      "Orca",
      "Eel",
    ]);
    expect(accountView(again).add.disabled).toBe(false);
  });
});

describe("a bot name's URL", () => {
  test("each name shows its URL; one registered before names had URLs shows none", () => {
    const old = { ...competitor("Old", 1500, 0), url: null };
    const view = accountView(account([...three, old]));
    expect(view.bots.map((row) => row.url)).toEqual([
      "https://bot.example.com/Kraken",
      "https://bot.example.com/Squid",
      "https://bot.example.com/Narwhal",
      null,
    ]);
    expect(noBotUrlSentence).toBe(
      "Add the URL your bot runs at to play it in rated lobbies.",
    );
  });

  test("a new name says its URL becomes public after its first rated game", () => {
    expect(botUrlPublicSentence).toContain(
      "Once the name has played a rated game, the leaderboard shows the URL",
    );
    // A listed name's change shows on the leaderboard; an unlisted one's isn't public yet.
    expect(botUrlEditHint(three[0] as Competitor)).toStartWith(
      "The leaderboard shows Kraken's new URL within a minute.",
    );
    expect(botUrlEditHint(three[2] as Competitor)).toContain(
      "Once the name has played a rated game",
    );
  });

  test("is checked far enough to say what is wrong before sending it", () => {
    expect(validateBotUrl("https://kraken.example.org/bot")).toBeUndefined();
    expect(validateBotUrl("  https://kraken.example.org  ")).toBeUndefined();
    expect(validateBotUrl("")).toBe(
      "Enter the URL your hosted bot runs at, such as https://bot.example.com/turn.",
    );
    expect(validateBotUrl("kraken.example.org")).toBe(
      "Enter a whole URL, such as https://bot.example.com/turn.",
    );
    expect(validateBotUrl("ftp://kraken.example.org")).toBe(
      "Use an https:// URL, such as https://bot.example.com/turn.",
    );
    // The game decides http://, which it takes only on its own machine.
    expect(validateBotUrl("http://127.0.0.1:41414/turn")).toBeUndefined();
  });

  test("a changed URL replaces the name's, keeping its place", () => {
    const changed = withChangedBotName(account(three), {
      ...(three[1] as Competitor),
      url: "https://squid.example.org/",
    });
    expect(changed.bot_names.map((bot) => bot.url)).toEqual([
      "https://bot.example.com/Kraken",
      "https://squid.example.org/",
      "https://bot.example.com/Narwhal",
    ]);
  });

  test("a URL the game refuses shows its sentence", () => {
    expect(
      botNameProblem(
        problem({
          status: 422,
          detail: "a hosted bot's URL must use https://",
        }),
      ),
    ).toBe("A hosted bot's URL must use https://.");
  });
});

describe("confirmations", () => {
  test("deleting a bot name says its rating goes with it", () => {
    expect(deleteBotNameConfirmation(three[0] as Competitor)).toEqual({
      question: "Delete the bot name Kraken?",
      text: "Its rating (1609, after 52 rated games) is deleted with it and can't be restored. Anyone can then register the name.",
      confirm: "Delete Kraken",
    });
    expect(deleteBotNameConfirmation(three[2] as Competitor).text).toContain(
      "(1500, no rated games yet)",
    );
  });

  test("deleting the account names what is deleted", () => {
    expect(deleteAccountConfirmation(account(three))).toEqual({
      question: "Delete your account?",
      text: "Your display name, your 3 bot names and every rating are deleted, and can't be restored. You can still play as a guest.",
      confirm: "Delete account",
    });
    expect(deleteAccountConfirmation(account([])).text).toStartWith(
      "Your display name and every rating are deleted",
    );
    expect(
      deleteAccountConfirmation(account(three.slice(0, 1))).text,
    ).toStartWith("Your display name, your bot name and every rating");
    expect(deleteAccountConfirmation(null).text).toStartWith(
      "Your display name, your bot names and every rating",
    );
  });
});

describe("problems", () => {
  test("a taken bot name shows the server's sentence", () => {
    const taken = problem({
      problemType: "urn:island-battles:problem:bot-name-taken",
      detail:
        'the bot name "Leviathan" is taken: names are unique across all accounts, without regard to case',
    });
    expect(botNameProblem(taken)).toBe(
      'The bot name "Leviathan" is taken: names are unique across all accounts, without regard to case.',
    );
  });

  test("a sixth name and a name that breaks the rule show the server's sentence too", () => {
    expect(
      botNameProblem(
        problem({
          problemType: "urn:island-battles:problem:bot-name-limit",
          detail:
            "an account has at most 5 bot names: delete one to register another",
        }),
      ),
    ).toBe(
      "An account has at most 5 bot names: delete one to register another.",
    );
    expect(
      botNameProblem(problem({ status: 422, detail: "a bot name is short" })),
    ).toBe("A bot name is short.");
  });

  test("a taken display name has the page's own sentence", () => {
    expect(
      displayNameProblem(
        problem({ code: "display_name_taken", detail: "display name taken" }),
      ),
    ).toBe("Another account has that display name. Choose another.");
    expect(
      displayNameProblem(problem({ status: 422, code: "invalid_input" })),
    ).toBe("Use 3 to 24 letters, digits, spaces, - and _.");
  });

  test("an ended session, a lost connection and a guest's token", () => {
    const ended = new ApiError({ kind: "problem", status: 401 });
    expect(endsSession(ended)).toBe(true);
    expect(accountProblem(ended)).toBe(
      "Your session has ended. Sign in again to continue.",
    );
    expect(
      accountProblem(new ApiError({ kind: "transport", status: 0 })),
    ).toStartWith("Cannot reach the server.");
    expect(
      accountProblem(
        problem({
          status: 403,
          problemType: "urn:island-battles:problem:sign-in-required",
        }),
      ),
    ).toBe("Sign in with Google to have an account.");
    expect(endsSession(problem({}))).toBe(false);
  });

  test("serverSentence", () => {
    expect(serverSentence("done")).toBe("Done.");
    expect(serverSentence("Done!")).toBe("Done!");
    expect(serverSentence("")).toBe("");
  });
});
