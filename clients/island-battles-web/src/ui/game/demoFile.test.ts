import { describe, expect, test } from "bun:test";
import {
  DemoFileError,
  describeDemo,
  fileSizeProblem,
  maxDemoBytes,
  readDemo,
  readDemoFile,
} from "./demoFile";
import { sampleDemo } from "./testing";

const expected = { format: "island-battles-demo", schemaVersion: 1 };

function rejects(file: unknown, pattern: RegExp, name: string) {
  const text = typeof file === "string" ? file : JSON.stringify(file);
  try {
    readDemo(text, expected);
  } catch (error) {
    expect(error).toBeInstanceOf(DemoFileError);
    expect((error as Error).message, name).toMatch(pattern);
    return;
  }
  throw new Error(`${name}: the file was accepted`);
}

describe("readDemo", () => {
  test("a demo opens as it was saved", () => {
    expect(readDemo(JSON.stringify(sampleDemo()))).toEqual(sampleDemo());
    // A demo of a running game has no outcome, and the saver's team only.
    const running = { ...sampleDemo(), outcome: null };
    running.logs = running.logs.map((logs) => ({
      ...logs,
      players: logs.players.slice(0, 1),
      hidden_team_colors: [2],
    }));
    expect(readDemo(JSON.stringify(running))).toEqual(running);
    expect(readDemo(JSON.stringify(sampleDemo(), null, 2))).toEqual(
      sampleDemo(),
    );
  });

  test("files that aren't demos are refused plainly", () => {
    const files: Record<string, string> = {
      "empty file": "",
      "not JSON": "<html>not a demo</html>",
      "a JSON list": "[]",
      "a JSON string": '"island-battles-demo"',
      null: "null",
    };
    for (const [name, file] of Object.entries(files)) {
      rejects(file, /not an Island Battles demo/, name);
    }
    rejects(
      { map_name: "Islands", castles: [] },
      /not an Island Battles demo\. Open a file saved with Save demo/,
      "a map file",
    );
    rejects(
      { ...sampleDemo(), format: "other-game-demo" },
      /not an Island Battles demo/,
      "another format",
    );
  });

  test("demos of other format versions are refused with their version", () => {
    rejects(
      { ...sampleDemo(), schema_version: 0 },
      /not a version number/,
      "version 0",
    );
    rejects(
      { ...sampleDemo(), schema_version: 2 },
      /a later version of Island Battles \(demo format version 2\)\. This viewer opens only version 1\./,
      "a later version",
    );
    rejects(
      { ...sampleDemo(), schema_version: "1" },
      /not a version number/,
      "a version as text",
    );
    rejects(
      { ...sampleDemo(), schema_version: undefined },
      /not a version number/,
      "no version",
    );
    const older = { format: "island-battles-demo", schema_version: 1 };
    expect(() =>
      readDemo(JSON.stringify(older), { ...expected, schemaVersion: 2 }),
    ).toThrow(
      /an earlier version of Island Battles \(demo format version 1\)\. This viewer opens only version 2\./,
    );
  });

  test("damaged demos name the value that is wrong", () => {
    // biome-ignore lint/suspicious/noExplicitAny: the cases damage a demo on purpose
    type Loose = any;
    const cases: [(file: Loose) => void, RegExp][] = [
      [
        (f) => {
          f.game_id = 0;
        },
        /game_id must be a whole number of at least 1/,
      ],
      [
        (f) => {
          f.saved_at = "yesterday";
        },
        /saved_at must be a date/,
      ],
      [
        (f) => {
          delete f.teams;
        },
        /teams must be a list/,
      ],
      [
        (f) => {
          f.teams[1].color = 1;
        },
        /teams\[1\]\.color repeats team colour 1/,
      ],
      [
        (f) => {
          f.teams[0].players = [7];
        },
        /teams\[0\]\.players\[0\] must be text/,
      ],
      [
        (f) => {
          f.outcome = "Blue wins";
        },
        /outcome must be an object/,
      ],
      [
        (f) => {
          f.outcome.summary = null;
        },
        /outcome\.summary must be text/,
      ],
      [
        (f) => {
          f.frames = [];
        },
        /frames must hold at least one generation/,
      ],
      [
        (f) => {
          f.frames[1].id = 41;
        },
        /frames\[1\]\.id must follow the generation before it/,
      ],
      [
        (f) => {
          f.frames[0].castles[1].x = "2";
        },
        /frames\[0\]\.castles\[1\]\.x must be a number/,
      ],
      [
        (f) => {
          f.frames[0].castles[0].soldiers = -1;
        },
        /frames\[0\]\.castles\[0\]\.soldiers must be a whole number/,
      ],
      [
        (f) => {
          f.frames[0].castles[0].upgrades = { speed: 0 };
        },
        /frames\[0\]\.castles\[0\]\.upgrades\.speed must be a whole number of at least 1/,
      ],
      [
        (f) => {
          f.frames[0].castles[0].upgrades = null;
        },
        /frames\[0\]\.castles\[0\]\.upgrades must be an object/,
      ],
      [
        (f) => {
          f.frames[0].castles[1].x = 1e308;
        },
        /frames\[0\]\.castles\[1\]\.x must be from -1000000 to 1000000/,
      ],
      [
        (f) => {
          f.frames[1].soldier_groups[0].y = -2e6;
        },
        /frames\[1\]\.soldier_groups\[0\]\.y must be from -1000000 to 1000000/,
      ],
      [
        (f) => {
          f.frames[1].clashes[0].participants[0].clash_radius = 1e7;
        },
        /frames\[1\]\.clashes\[0\]\.participants\[0\]\.clash_radius must be more than 0 and at most 1000000/,
      ],
      [
        (f) => {
          f.logs[0].players[1].team_color = 3;
        },
        /logs\[0\]\.players\[1\]\.team_color must be one of the teams' colours/,
      ],
      [
        (f) => {
          f.frames[1].soldier_groups[0].clash_radius = 0;
        },
        /frames\[1\]\.soldier_groups\[0\]\.clash_radius must be more than 0 and at most 1000000/,
      ],
      [
        (f) => {
          f.frames[1].clashes[0].participants = {};
        },
        /frames\[1\]\.clashes\[0\]\.participants must be a list/,
      ],
      [
        (f) => {
          f.frames[1].clashes[0].participants[0].soldiers_after = 1.5;
        },
        /frames\[1\]\.clashes\[0\]\.participants\[0\]\.soldiers_after must be a whole number/,
      ],
      [
        (f) => {
          f.logs.pop();
        },
        /logs must hold one entry for each of the 2 frames, and it holds 1/,
      ],
      [
        (f) => {
          f.logs[1].generation_id = 41;
        },
        /logs\[1\]\.generation_id must be frames\[1\]\.id, 42/,
      ],
      [
        (f) => {
          f.logs[0].hidden_team_colors = null;
        },
        /logs\[0\]\.hidden_team_colors must be a list/,
      ],
      [
        (f) => {
          f.logs[0].players[0].lines[0].level = "fatal";
        },
        /logs\[0\]\.players\[0\]\.lines\[0\]\.level must be debug, info, warning or error/,
      ],
      [
        (f) => {
          f.logs[0].players[0].lines[0].message = { text: "hi" };
        },
        /logs\[0\]\.players\[0\]\.lines\[0\]\.message must be text/,
      ],
    ];
    for (const [change, pattern] of cases) {
      const file = sampleDemo();
      change(file);
      rejects(
        file,
        new RegExp(`damaged and can’t be shown: ${pattern.source}`),
        pattern.source,
      );
    }
  });

  test("markup in a demo stays text", () => {
    const file = sampleDemo();
    const team = file.teams[0];
    if (!team) throw new Error("no team");
    team.name = "<img src=x onerror=alert(1)>";
    expect(readDemo(JSON.stringify(file)).teams[0]?.name).toBe(
      "<img src=x onerror=alert(1)>",
    );
  });
});

describe("file checks", () => {
  test("files larger than any demo are refused before they are read", () => {
    expect(fileSizeProblem(1024)).toBeNull();
    expect(fileSizeProblem(maxDemoBytes)).toBeNull();
    expect(fileSizeProblem(maxDemoBytes + 1)).toMatch(/more than any demo/);
  });

  test("readDemoFile reads a local file and explains a wrong one", async () => {
    const good = new File([JSON.stringify(sampleDemo())], "demo.json");
    expect((await readDemoFile(good)).game_id).toBe(12);
    const wrong = new File(["hello"], "notes.txt");
    await expect(readDemoFile(wrong)).rejects.toThrow(/it is not JSON/);
    const later = new File(
      [JSON.stringify({ ...sampleDemo(), schema_version: 2 })],
      "later.json",
    );
    await expect(readDemoFile(later)).rejects.toThrow(/a later version/);
  });

  test("the summary says when a demo comes from an unfinished game", () => {
    expect(describeDemo(sampleDemo())).toMatch(
      /^Game 12 · 2 generations · saved .+\. The game had ended\.$/,
    );
    expect(describeDemo({ ...sampleDemo(), outcome: null })).toMatch(
      /Saved during the game, before it ended/,
    );
  });
});
