import { describe, expect, test } from "bun:test";
import { mergeGenerations, sortTeams, teamStats, turnLabel } from "./history";
import { generation, sampleDemo } from "./testing";

const ids = (frames: readonly { id: number }[]) =>
  frames.map((frame) => frame.id);

describe("mergeGenerations", () => {
  test("appends new generations in id order", () => {
    const held = [generation(10), generation(12)];
    expect(
      ids(mergeGenerations(held, [generation(15), generation(13)])),
    ).toEqual([10, 12, 13, 15]);
  });

  test("ignores generations already held, keeping the held one", () => {
    const held = [generation(10, 30), generation(12, 30)];
    const merged = mergeGenerations(held, [generation(12, 99)]);
    expect(merged).toBe(held);
    expect(merged[1]?.castles[0]?.soldiers).toBe(30);
  });

  test("fills a gap behind the newest turn, as after a reconnect", () => {
    const held = [generation(10), generation(14)];
    const merged = mergeGenerations(held, [
      generation(10),
      generation(11),
      generation(12),
      generation(14),
    ]);
    expect(ids(merged)).toEqual([10, 11, 12, 14]);
  });

  test("ids with gaps are only ordered, never counted", () => {
    const merged = mergeGenerations([], [generation(7), generation(300)]);
    expect(turnLabel(1, merged.length)).toBe("Turn 2 / 2");
  });

  test("a repeated id inside one batch counts once", () => {
    expect(ids(mergeGenerations([], [generation(1), generation(1)]))).toEqual([
      1,
    ]);
  });
});

describe("turnLabel", () => {
  test("numbers turns by index from 1", () => {
    expect(turnLabel(36, 120)).toBe("Turn 37 / 120");
    expect(turnLabel(0, 1)).toBe("Turn 1 / 1");
  });
});

describe("teamStats", () => {
  test("counts castles and sums castle and travelling soldiers per team", () => {
    const demo = sampleDemo();
    const frame = demo.frames[1];
    if (!frame) throw new Error("no frame");
    const [blue, red] = teamStats(frame, demo.teams);
    expect(blue).toMatchObject({ castles: 1, soldiers: 30 });
    expect(red).toMatchObject({ castles: 1, soldiers: 30 });
  });

  test("a team without castles has none", () => {
    const frame = generation(1);
    frame.castles = frame.castles.filter((castle) => castle.team_color === 1);
    const stats = teamStats(frame, sampleDemo().teams);
    expect(stats[1]).toMatchObject({ castles: 0, soldiers: 0 });
  });

  test("sortTeams orders by colour", () => {
    const teams = [...sampleDemo().teams].reverse();
    expect(sortTeams(teams).map((team) => team.color)).toEqual([1, 2]);
  });
});
