import { describe, expect, test } from "bun:test";
import {
  botNameLocked,
  botNameOfValue,
  botNameValue,
  guestBotNamesHint,
  keptName,
  nameWithoutUrlHint,
  noBotNamesHint,
  ratedChoice,
  ratedChoiceValue,
  ratedKindsHint,
} from "./botPicker";

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
    expect(nameWithoutUrlHint).toBe(
      "No URL yet, so it can't play rated games.",
    );
  });
});

test("an unrated lobby keeps a saved name when the same kind of bot is saved again, and offers none", () => {
  const kraken = { kind: "localhost" as const, name: "Kraken" };
  expect(keptName("localhost", kraken)).toBe("Kraken");
  expect(keptName("hosted", kraken)).toBe("");
  expect(keptName("manual", kraken)).toBe("");
  expect(keptName("hosted", { kind: "hosted", name: "Kraken" })).toBe("Kraken");
  expect(keptName("localhost", undefined)).toBe("");
});
