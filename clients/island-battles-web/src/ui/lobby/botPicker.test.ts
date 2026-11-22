import { describe, expect, test } from "bun:test";
import {
  botNameLocked,
  botNameOfValue,
  botNameValue,
  chosenUnlistedLabel,
  guestBotNamesHint,
  keptName,
  leaderboardChoice,
  leaderboardLocked,
  leaderboardRatedHint,
  leaderboardUnratedHint,
  listedBotOptions,
  nameWithoutUrlHint,
  noBotNamesHint,
  pickableListedBots,
  ratedChoice,
  ratedChoiceValue,
  ratedKindsHint,
  unratedChoiceValue,
  unratedNoBotNamesHint,
} from "./botPicker";
import { choiceValue, leaderboardValue } from "./roomModel";

const names = [
  { name: "Kraken", url: "https://kraken.example.org/bot" },
  { name: "Old", url: null },
];

describe("a rated lobby's choices", () => {
  test("a bot name saves a hosted bot under it, with no URL to type", () => {
    expect(ratedChoice("Kraken")).toEqual({ kind: "hosted", name: "Kraken" });
    expect(botNameValue("Kraken")).toBe("botname:Kraken");
    expect(botNameOfValue("botname:Kraken")).toBe("Kraken");
    expect(botNameOfValue("botname:")).toBeUndefined();
    expect(botNameOfValue("hosted")).toBeUndefined();
    expect(botNameOfValue("manual")).toBeUndefined();
  });

  test("checks hand play, or the name a hosted bot plays as, without regard to case", () => {
    expect(ratedChoiceValue({ kind: "manual" }, names)).toBe("manual");
    expect(ratedChoiceValue({ kind: "hosted", name: "kraken" }, names)).toBe(
      "botname:Kraken",
    );
    // A url beside the name is ignored, as a rated lobby ignores it.
    expect(
      ratedChoiceValue(
        { kind: "hosted", name: "Kraken", url: "https://typed.example" },
        names,
      ),
    ).toBe("botname:Kraken");
  });

  test("checks nothing for a choice a rated lobby refuses, or a deleted name", () => {
    expect(ratedChoiceValue(undefined, names)).toBe("");
    expect(ratedChoiceValue({ kind: "localhost", name: "Kraken" }, names)).toBe(
      "",
    );
    expect(
      ratedChoiceValue(
        { kind: "uploaded", package_id: "pk", name: "Kraken" },
        names,
      ),
    ).toBe("");
    expect(ratedChoiceValue({ kind: "default", level: 2 }, names)).toBe("");
    expect(
      ratedChoiceValue({ kind: "hosted", url: "https://b.example" }, names),
    ).toBe("");
    expect(ratedChoiceValue({ kind: "hosted", name: "Octopus" }, names)).toBe(
      "",
    );
    expect(ratedChoiceValue({ kind: "hosted", name: "Kraken" }, null)).toBe("");
    // One of your names chosen by its ID checks that name.
    expect(
      ratedChoiceValue({ kind: "hosted", competitor_id: "c-kraken" }, [
        {
          name: "Kraken",
          url: "https://kraken.example.org/bot",
          id: "c-kraken",
        },
      ]),
    ).toBe("botname:Kraken");
  });

  test("a name without a URL can't be chosen, nor any name while the server plays no hosted bots", () => {
    const free = { locked: false, saving: false, serverBots: true };
    const [kraken, old] = names as [(typeof names)[0], (typeof names)[1]];
    expect(botNameLocked(kraken, free)).toBe(false);
    expect(botNameLocked(old, free)).toBe(true);
    expect(botNameLocked(kraken, { ...free, serverBots: false })).toBe(true);
    // Until GET /bots/defaults answers, a name can be chosen; the server decides.
    expect(botNameLocked(kraken, { ...free, serverBots: undefined })).toBe(
      false,
    );
    expect(botNameLocked(kraken, { ...free, saving: true })).toBe(true);
    expect(botNameLocked(kraken, { ...free, locked: true })).toBe(true);
  });

  test("its sentences", () => {
    expect(ratedKindsHint).toBe(
      "Rated bots run at their bot name's URL, so localhost and uploaded bots aren't offered here, and default bots play only in the host's bot seats.",
    );
    expect(noBotNamesHint).toBe(
      "Register a bot name with its hosted bot's URL in your account to play rated games with a bot.",
    );
    expect(guestBotNamesHint).toBe(
      "Sign in with Google to play a rated game with one of your bot names.",
    );
    expect(nameWithoutUrlHint).toBe("No URL yet, so it can't play.");
  });
});

test("an unrated lobby keeps a saved name when the same kind of bot is saved again, and offers none", () => {
  const kraken = { kind: "localhost" as const, name: "Kraken" };
  expect(keptName("localhost", kraken)).toBe("Kraken");
  expect(keptName("hosted", kraken)).toBe("");
  expect(keptName("manual", kraken)).toBe("");
  expect(
    keptName("hosted", {
      kind: "hosted",
      name: "Kraken",
      url: "https://a.example.org",
    }),
  ).toBe("Kraken");
  // A pick of the name itself: a URL typed after it plays under no name (#410).
  expect(keptName("hosted", { kind: "hosted", name: "Kraken" })).toBe("");
  expect(keptName("localhost", undefined)).toBe("");
});

describe("a bot from the leaderboard", () => {
  const listed = [
    {
      competitorId: "c-kraken",
      name: "Kraken",
      owner: "Dana",
      url: "https://bot.example.org/turn",
      rating: 1520,
    },
    {
      competitorId: "c-eel",
      name: "Eel",
      owner: "Sam",
      url: "https://bot.example.org/turn",
      rating: 1520,
    },
  ];

  test("saves a hosted bot by its competitor ID, with no name or URL", () => {
    expect(leaderboardChoice("c-eel")).toEqual({
      kind: "hosted",
      competitor_id: "c-eel",
    });
    expect(leaderboardValue).toBe("leaderboard");
  });

  test("checks its radio in a rated lobby and in one that isn't", () => {
    const chosen = leaderboardChoice("c-eel");
    expect(ratedChoiceValue(chosen, names)).toBe(leaderboardValue);
    expect(ratedChoiceValue(chosen, null)).toBe(leaderboardValue);
    expect(choiceValue(chosen)).toBe(leaderboardValue);
    // A hosted bot at a URL is still the URL's radio.
    expect(choiceValue({ kind: "hosted", url: "https://b.example" })).toBe(
      "hosted",
    );
  });

  test("lists every listed bot but your own names, which it offers by name", () => {
    expect(
      pickableListedBots(listed, [
        {
          name: "Kraken",
          url: "https://kraken.example.org/bot",
          id: "c-kraken",
        },
      ]),
    ).toEqual([
      {
        competitorId: "c-eel",
        name: "Eel",
        owner: "Sam",
        url: "https://bot.example.org/turn",
        rating: 1520,
      },
    ]);
    expect(pickableListedBots(listed, null)).toEqual(listed);
    // Your own bot, chosen by its ID, stays listed.
    expect(
      pickableListedBots(
        listed,
        [
          {
            name: "Kraken",
            url: "https://kraken.example.org/bot",
            id: "c-kraken",
          },
        ],
        "c-kraken",
      ),
    ).toEqual(listed);
  });

  test("its select names each bot with its owner, and keeps a chosen one it doesn't list", () => {
    expect(listedBotOptions(listed, undefined)).toEqual([
      { value: "", label: "Choose a bot…" },
      { value: "c-kraken", label: "Kraken, by Dana (rated 1520)" },
      { value: "c-eel", label: "Eel, by Sam (rated 1520)" },
    ]);
    expect(listedBotOptions(listed, "c-eel")).toEqual([
      { value: "c-kraken", label: "Kraken, by Dana (rated 1520)" },
      { value: "c-eel", label: "Eel, by Sam (rated 1520)" },
    ]);
    expect(listedBotOptions(listed, "c-gone")[0]).toEqual({
      value: "c-gone",
      label: chosenUnlistedLabel,
    });
  });

  test("can't be chosen while locked or saving, without hosted bots, or with no bot to choose", () => {
    const free = { locked: false, saving: false, serverBots: true, offered: 2 };
    expect(leaderboardLocked(free)).toBe(false);
    expect(leaderboardLocked({ ...free, serverBots: undefined })).toBe(false);
    expect(leaderboardLocked({ ...free, serverBots: false })).toBe(true);
    expect(leaderboardLocked({ ...free, offered: 0 })).toBe(true);
    expect(leaderboardLocked({ ...free, locked: true })).toBe(true);
    expect(leaderboardLocked({ ...free, saving: true })).toBe(true);
  });

  test("says whose rating a rated game changes", () => {
    expect(leaderboardRatedHint).toContain("rated in your place");
    expect(leaderboardUnratedHint).toBe(
      "Anyone's registered bot, rated or not, plays for your team at its URL, without its owner. The server plays it; this tab relays nothing.",
    );
  });
});

describe("your bot names in a lobby that isn't rated (#410)", () => {
  const own = [
    { name: "Kraken", url: "https://kraken.example.org/bot", id: "c-kraken" },
    { name: "Old", url: null, id: "c-old" },
  ];

  test("choosing one saves a hosted bot under it, with no URL to type", () => {
    expect(ratedChoice("Kraken")).toEqual({ kind: "hosted", name: "Kraken" });
  });

  test("a saved choice under a name, or by its competitor ID, checks the name's radio", () => {
    expect(unratedChoiceValue({ kind: "hosted", name: "kraken" }, own)).toBe(
      botNameValue("Kraken"),
    );
    expect(
      unratedChoiceValue({ kind: "hosted", competitor_id: "c-kraken" }, own),
    ).toBe(botNameValue("Kraken"));
  });

  test("a hosted bot at a typed URL checks My hosted bot (URL), even with a name kept", () => {
    expect(
      unratedChoiceValue(
        { kind: "hosted", name: "Kraken", url: "https://other.example.org" },
        own,
      ),
    ).toBe("hosted");
    expect(
      unratedChoiceValue({ kind: "hosted", url: "https://b.example" }, own),
    ).toBe("hosted");
  });

  test("someone else's registered bot, and every other kind, check as before", () => {
    expect(
      unratedChoiceValue({ kind: "hosted", competitor_id: "c-eel" }, own),
    ).toBe(leaderboardValue);
    expect(unratedChoiceValue({ kind: "default", level: 2 }, own)).toBe(
      "level-2",
    );
    expect(unratedChoiceValue({ kind: "localhost", name: "Kraken" }, own)).toBe(
      "localhost",
    );
    expect(unratedChoiceValue(undefined, own)).toBe("");
  });

  test("names not read yet, or a deleted name, check no radio", () => {
    expect(unratedChoiceValue({ kind: "hosted", name: "Kraken" }, null)).toBe(
      "",
    );
    expect(unratedChoiceValue({ kind: "hosted", name: "Gone" }, own)).toBe("");
    expect(
      unratedChoiceValue({ kind: "hosted", competitor_id: "c-kraken" }, null),
    ).toBe(leaderboardValue);
  });

  test("a name without a URL can't be chosen, and says where to add one", () => {
    const state = { locked: false, saving: false, serverBots: true };
    expect(botNameLocked(own[1] as (typeof own)[number], state)).toBe(true);
    expect(botNameLocked(own[0] as (typeof own)[number], state)).toBe(false);
    expect(unratedNoBotNamesHint).toBe(
      "Register a bot name with its hosted bot's URL in your account to choose it here in one click.",
    );
  });
});
