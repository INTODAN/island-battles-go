import { describe, expect, test } from "bun:test";
import {
  noBotNamesHint,
  playsAsLocked,
  playsAsName,
  playsAsOptions,
  ratedDefaultsHint,
  takesBotName,
} from "./botPicker";

describe("Plays as", () => {
  test("offers No name first, then your bot names", () => {
    expect(playsAsOptions(["Kraken", "Squid"], "")).toEqual([
      { value: "", label: "No name" },
      { value: "Kraken", label: "Kraken" },
      { value: "Squid", label: "Squid" },
    ]);
    // A chosen name is one of yours without regard to case.
    expect(playsAsOptions(["Kraken"], "kraken")).toHaveLength(2);
  });

  test("keeps a chosen name that was deleted, marked, so the select shows what is saved", () => {
    expect(playsAsOptions(["Kraken"], "Octopus").at(-1)).toEqual({
      value: "Octopus",
      label: "Octopus (deleted)",
    });
  });

  test("belongs to the localhost, hosted and uploaded choices only", () => {
    expect(takesBotName("localhost")).toBe(true);
    expect(takesBotName("hosted")).toBe(true);
    expect(takesBotName("package-pk")).toBe(true);
    expect(takesBotName("manual")).toBe(false);
    expect(takesBotName("level-2")).toBe(false);
  });

  test("its sentences", () => {
    expect(noBotNamesHint).toBe(
      "Register a bot name in your account to play rated games with a bot.",
    );
    expect(ratedDefaultsHint).toBe(
      "Default bots play only in bot seats in a rated lobby.",
    );
  });
});

describe("the name a choice plays as", () => {
  const kraken = { kind: "localhost" as const, name: "Kraken" };
  const rated = { rated: true, signedIn: true };

  test("carries over to another of your bots in a rated lobby", () => {
    expect(playsAsName("localhost", kraken, undefined, rated)).toBe("Kraken");
    expect(playsAsName("hosted", kraken, undefined, rated)).toBe("Kraken");
    expect(playsAsName("package-pk", kraken, undefined, rated)).toBe("Kraken");
    // Plays as, changed and not saved yet, wins; "" is No name.
    expect(playsAsName("hosted", kraken, "Squid", rated)).toBe("Squid");
    expect(playsAsName("hosted", kraken, "", rated)).toBe("");
  });

  test("is none for hand play, a default bot and a guest", () => {
    expect(playsAsName("manual", kraken, undefined, rated)).toBe("");
    expect(playsAsName("level-2", kraken, undefined, rated)).toBe("");
    expect(
      playsAsName("hosted", kraken, "Squid", { rated: true, signedIn: false }),
    ).toBe("");
  });

  test("stays with its own choice in an unrated lobby", () => {
    const unrated = { rated: false, signedIn: true };
    expect(playsAsName("localhost", kraken, undefined, unrated)).toBe("Kraken");
    expect(playsAsName("hosted", kraken, undefined, unrated)).toBe("");
  });
});

test("a package greyed out this month can't be chosen through its Plays as", () => {
  const free = { locked: false, saving: false, computeSpent: null };
  const spent = {
    ...free,
    computeSpent: "Uploaded bots have used this month's free compute.",
  };
  expect(playsAsLocked("package-pk", free)).toBe(false);
  expect(playsAsLocked("package-pk", spent)).toBe(true);
  // Only uploaded bots use the server's compute.
  expect(playsAsLocked("localhost", spent)).toBe(false);
  expect(playsAsLocked("hosted", { ...free, saving: true })).toBe(true);
  expect(playsAsLocked("hosted", { ...free, locked: true })).toBe(true);
});
