import { describe, expect, test } from "bun:test";
import {
  computeSpentMessage,
  offeredChoice,
  seatOptions,
  uploadedGreyedOut,
} from "./compute";

describe("the monthly guard on uploaded bots", () => {
  test("says nothing while uploaded bots may play, or before the server says", () => {
    expect(computeSpentMessage({ month: "2026-11", available: true })).toBe(
      null,
    );
    expect(computeSpentMessage(null)).toBe(null);
  });

  test("names the first of the next month once the compute is spent, as the server does", () => {
    expect(computeSpentMessage({ month: "2026-11", available: false })).toBe(
      "Uploaded bots have used this month's free compute. They're back on 1 December.",
    );
    expect(computeSpentMessage({ month: "2026-12", available: false })).toBe(
      "Uploaded bots have used this month's free compute. They're back on 1 January.",
    );
  });

  test("greys out your packages in the bot picker once spent, and no other choice", () => {
    const spent = "Uploaded bots have used this month's free compute.";
    for (const value of ["localhost", "level-1", "hosted"]) {
      expect(uploadedGreyedOut(value, spent)).toBe(false);
    }
    expect(uploadedGreyedOut("package-p-1", spent)).toBe(true);
    expect(uploadedGreyedOut("package-p-1", null)).toBe(false);
  });

  const packages = [
    { package_id: "p-1", owner_name: "Uma", language: "python" },
    { package_id: "p-2", owner_name: "Sam", language: "native" },
  ];

  test("greys out every package in Add bot once spent, and nothing else", () => {
    const spent = computeSpentMessage({ month: "2026-11", available: false });
    expect(
      seatOptions({ levels: [1, 2], serverBots: true, packages, spent }),
    ).toEqual([
      { value: "level-1", label: "Bot level 1", disabled: false },
      { value: "level-2", label: "Bot level 2", disabled: false },
      {
        value: "package-p-1",
        label: "Uploaded: Uma's bot (python)",
        disabled: true,
      },
      {
        value: "package-p-2",
        label: "Uploaded: Sam's bot (native)",
        disabled: true,
      },
      { value: "hosted", label: "Hosted bot (URL)", disabled: false },
    ]);
    expect(
      seatOptions({ levels: [1], serverBots: true, packages, spent: null })
        .filter((choice) => choice.disabled)
        .map((choice) => choice.value),
    ).toEqual([]);
    expect(
      seatOptions({ levels: [1], serverBots: false, packages, spent }).map(
        (choice) => choice.value,
      ),
    ).toEqual(["level-1"]);
  });

  test("a greyed-out choice falls back to the first one offered", () => {
    const choices = seatOptions({
      levels: [1, 2],
      serverBots: true,
      packages,
      spent: "spent",
    });
    expect(offeredChoice(choices, "package-p-1")).toBe("level-1");
    expect(offeredChoice(choices, "level-2")).toBe("level-2");
    expect(offeredChoice(choices, "hosted")).toBe("hosted");
    expect(offeredChoice(choices, "package-gone")).toBe("level-1");
  });
});
