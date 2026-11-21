import { describe, expect, test } from "bun:test";
import {
  noBotNamesHint,
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
