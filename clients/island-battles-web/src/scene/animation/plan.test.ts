import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../../api/generated/game";
import { CASTLE_RADIUS } from "../board";
import { castleDisplayLevel } from "../castleLevel";
import { toMapDistance } from "../coordinates";
import {
  type MoveTrack,
  planTurn,
  sampleTurn,
  speedOf,
  type Track,
  type TurnPlan,
} from "./plan";

type Demo = components["schemas"]["Demo"];
type Generation = components["schemas"]["Generation"];

function load(name: string): Demo {
  return JSON.parse(
    readFileSync(
      new URL(`../../../test/fixtures/${name}.json`, import.meta.url),
      "utf8",
    ),
  ) as Demo;
}

const fixtures = {
  "generated-map": load("generated-map"),
  "busy-turn": load("busy-turn"),
  "six-teams": load("six-teams"),
};

/** Every consecutive pair of every fixture. */
const pairs = Object.entries(fixtures).flatMap(([name, demo]) =>
  demo.frames.slice(1).map((next, index) => ({
    name,
    index: index + 1,
    previous: demo.frames[index] as Generation,
    next,
  })),
);

function pair(name: keyof typeof fixtures, index: number) {
  const found = pairs.find(
    (entry) => entry.name === name && entry.index === index,
  );
  if (!found) throw new Error(`no pair ${name}[${index}]`);
  return { ...found, plan: planTurn(found.previous, found.next) };
}

function tracksOf<K extends Track["kind"]>(
  plan: TurnPlan,
  kind: K,
): Extract<Track, { kind: K }>[] {
  return plan.tracks.filter(
    (track): track is Extract<Track, { kind: K }> => track.kind === kind,
  );
}

function moveOf(plan: TurnPlan, groupId: number): MoveTrack {
  const track = tracksOf(plan, "move").find((t) => t.groupId === groupId);
  if (!track) throw new Error(`no move track for group ${groupId}`);
  return track;
}

const distance = (
  left: { x: number; y: number },
  right: { x: number; y: number },
) => Math.hypot(left.x - right.x, left.y - right.y);

describe("planTurn on fixture pairs", () => {
  test("a launch starts at the origin castle's edge and ends where the group now is", () => {
    // busy-turn frame 1: four groups leave their castles.
    const { previous, next, plan } = pair("busy-turn", 1);
    const launched = next.soldier_groups.filter(
      (group) => !previous.soldier_groups.some((old) => old.id === group.id),
    );
    expect(launched.length).toBeGreaterThan(0);
    for (const group of launched) {
      const origin = next.castles.find((c) => c.id === group.origin_castle_id);
      const move = moveOf(plan, group.id);
      const first = move.keys[0];
      expect(first?.t).toBe(0);
      expect(distance(first as never, origin as never)).toBeCloseTo(
        toMapDistance(CASTLE_RADIUS),
        6,
      );
      expect(move.keys.at(-1)).toMatchObject({ t: 1, x: group.x, y: group.y });
      expect(
        tracksOf(plan, "spawn").some((spawn) => spawn.groupId === group.id),
      ).toBe(true);
      // The soldiers leave the castle at t = 0: its count drops before anything lands.
      const count = tracksOf(plan, "castleCount").find(
        (track) => track.castleId === origin?.id,
      );
      const before = previous.castles.find((c) => c.id === origin?.id);
      expect(count?.keys[0]?.value).toBe(before?.soldiers ?? -1);
      expect(count?.keys[1]?.t).toBe(0.06);
      expect(count?.keys[1]?.value).toBeLessThan(before?.soldiers ?? 0);
    }
    expect(plan.transients).toHaveLength(0);
  });

  test("an arrival sails to the castle's edge, then into it, and the castle counts it", () => {
    // busy-turn frame 3: two groups arrive and nothing clashes.
    const { previous, next, plan } = pair("busy-turn", 3);
    const arriving = previous.soldier_groups.filter(
      (group) => !next.soldier_groups.some((now) => now.id === group.id),
    );
    expect(arriving).toHaveLength(2);
    for (const group of arriving) {
      const castle = next.castles.find(
        (c) => c.id === group.destination_castle_id,
      );
      const move = moveOf(plan, group.id);
      expect(move.keys[0]).toMatchObject({ t: 0, x: group.x, y: group.y });
      const merge = tracksOf(plan, "merge").find(
        (track) => track.groupId === group.id,
      );
      expect(merge?.castleId).toBe(castle?.id);
      const edge = move.keys.find((key) => key.t === merge?.t0);
      expect(distance(edge as never, castle as never)).toBeCloseTo(
        Math.min(
          toMapDistance(CASTLE_RADIUS),
          distance(group, castle as never),
        ),
        6,
      );
      expect(move.keys.at(-1)).toMatchObject({ x: castle?.x, y: castle?.y });
      expect(merge?.t1).toBeLessThanOrEqual(1);
      // Gone from the board once merged, and the sample before that still shows it.
      expect(sampleTurn(plan, 0).groups.get(group.id)?.visible).toBe(true);
      expect(sampleTurn(plan, 1).groups.get(group.id)?.visible).toBe(false);
    }
    expect(plan.transients.map((group) => group.id).sort()).toEqual(
      arriving.map((group) => group.id).sort(),
    );
  });

  test("a clash with one loser: the loser sinks after the contact, the winner keeps sailing", () => {
    // generated-map frame 4: a clash in which one group is defeated.
    const { next, previous, plan } = pair("generated-map", 4);
    const clash = next.clashes[0];
    expect(
      clash?.participants.filter((p) => p.soldiers_after === 0),
    ).toHaveLength(1);
    const loser = clash?.participants.find((p) => p.soldiers_after === 0);
    const winner = clash?.participants.find((p) => p.soldiers_after > 0);
    const sink = tracksOf(plan, "sink").find(
      (t) => t.groupId === loser?.group_id,
    );
    expect(sink?.t0).toBe(clash?.time as number);
    expect(sink?.t1).toBeGreaterThan(sink?.t0 as number);

    // The loser is at its contact position when it starts to sink.
    const lost = sampleTurn(plan, clash?.time as number).groups.get(
      loser?.group_id as number,
    );
    expect(lost).toMatchObject({ x: loser?.x, y: loser?.y, sink: 0 });
    expect(
      sampleTurn(plan, 1).groups.get(loser?.group_id as number)?.visible,
    ).toBe(false);
    expect(
      sampleTurn(plan, ((clash?.time as number) + 1) / 2).groups.get(
        loser?.group_id as number,
      )?.sink,
    ).toBeGreaterThan(0);

    // The winner passes through its contact position at clash.time and ends at the next position.
    const winnerNow = next.soldier_groups.find(
      (g) => g.id === winner?.group_id,
    );
    expect(previous.soldier_groups.some((g) => g.id === winner?.group_id)).toBe(
      false,
    );
    const through = sampleTurn(plan, clash?.time as number).groups.get(
      winner?.group_id as number,
    );
    expect(through).toMatchObject({ x: winner?.x, y: winner?.y });
    expect(through?.count).toBe(winner?.soldiers_before as number);
    const after = sampleTurn(plan, 1).groups.get(winner?.group_id as number);
    expect(after).toMatchObject({
      x: winnerNow?.x,
      y: winnerNow?.y,
      count: winnerNow?.soldiers,
    });
    expect(winnerNow?.soldiers).toBe(winner?.soldiers_after as number);

    // The effect fires at the clash time at each participant's position.
    const effect = tracksOf(plan, "clash")[0];
    expect(effect?.t).toBe(clash?.time as number);
    expect(effect?.points.map((p) => [p.x, p.y])).toEqual(
      clash?.participants.map((p) => [p.x, p.y]),
    );
    // Counts change at the clash: still `before` at the contact, `after` once the tick is done.
    expect(
      sampleTurn(plan, (clash?.time as number) - 0.001).flashes,
    ).toHaveLength(0);
    expect(
      sampleTurn(plan, (clash?.time as number) + 0.05).flashes,
    ).toHaveLength(clash?.participants.length as number);
  });

  test("a clash where both are defeated: both sink at the contact", () => {
    // generated-map frame 6: both groups lose everything.
    const { next, plan } = pair("generated-map", 6);
    const clash = next.clashes[0];
    expect(clash?.participants.every((p) => p.soldiers_after === 0)).toBe(true);
    expect(clash?.participants).toHaveLength(2);
    for (const participant of clash?.participants ?? []) {
      const sink = tracksOf(plan, "sink").find(
        (t) => t.groupId === participant.group_id,
      );
      expect(sink?.t0).toBe(clash?.time as number);
      const at = sampleTurn(plan, clash?.time as number).groups.get(
        participant.group_id,
      );
      expect(at).toMatchObject({ x: participant.x, y: participant.y });
      expect(at?.count).toBe(participant.soldiers_before);
      expect(
        sampleTurn(plan, 1).groups.get(participant.group_id)?.visible,
      ).toBe(false);
      expect(sampleTurn(plan, 1).groups.get(participant.group_id)?.count).toBe(
        0,
      );
    }
    expect(
      next.soldier_groups.some((g) =>
        clash?.participants.some((p) => p.group_id === g.id),
      ),
    ).toBe(false);
  });

  test("a capture flips the castle's team when the capturing group lands", () => {
    // busy-turn frame 2: a group arrives and takes a castle.
    const { previous, next, plan } = pair("busy-turn", 2);
    const captured = next.castles.find(
      (castle) =>
        castle.team_color !==
        previous.castles.find((c) => c.id === castle.id)?.team_color,
    );
    expect(captured).toBeDefined();
    const before = previous.castles.find((c) => c.id === captured?.id);
    const team = tracksOf(plan, "castleTeam").find(
      (t) => t.castleId === captured?.id,
    );
    expect(team?.keys).toHaveLength(2);
    const flip = team?.keys[1];
    expect(flip?.team).toBe(captured?.team_color as number);
    const merge = tracksOf(plan, "merge").find(
      (t) => t.castleId === captured?.id,
    );
    expect(flip?.t).toBe(merge?.t1 as number);
    const castleBefore = sampleTurn(
      plan,
      (flip?.t as number) - 0.001,
    ).castles.get(captured?.id as number);
    expect(castleBefore?.team).toBe(before?.team_color as number);
    expect(
      sampleTurn(plan, 1).castles.get(captured?.id as number),
    ).toMatchObject({
      team: captured?.team_color,
      count: captured?.soldiers,
    });
    // The count ticks to the new owner's remaining soldiers right where the team flips.
    const at = sampleTurn(plan, (flip?.t as number) + 0.07).castles.get(
      captured?.id as number,
    );
    expect(at?.team).toBe(captured?.team_color as number);
  });

  test("a group paused after a clash stays where it is for the whole turn", () => {
    // generated-map frame 5: a survivor of frame 4's clash is stopped.
    const { previous, next, plan } = pair("generated-map", 5);
    const paused = next.soldier_groups.filter((group) => {
      const old = previous.soldier_groups.find((g) => g.id === group.id);
      return old && old.x === group.x && old.y === group.y;
    });
    expect(paused).toHaveLength(1);
    const group = paused[0];
    expect(
      next.clashes.some((c) =>
        c.participants.some((p) => p.group_id === group?.id),
      ),
    ).toBe(false);
    const move = moveOf(plan, group?.id as number);
    for (const key of move.keys) {
      expect([key.x, key.y]).toEqual([group?.x as number, group?.y as number]);
    }
    for (const t of [0, 0.3, 0.7, 1]) {
      expect(sampleTurn(plan, t).groups.get(group?.id as number)).toMatchObject(
        {
          x: group?.x,
          y: group?.y,
          visible: true,
          sink: 0,
        },
      );
    }
  });

  test("a launched group that meets an enemy goes to the contact first, then to its next position", () => {
    // generated-map frame 4: the survivor was launched this turn and is stopped by the clash.
    const { next, previous, plan } = pair("generated-map", 4);
    const clash = next.clashes[0];
    const survivor = clash?.participants.find((p) => p.soldiers_after > 0);
    expect(
      previous.soldier_groups.some((g) => g.id === survivor?.group_id),
    ).toBe(false);
    const origin = next.castles.find(
      (c) =>
        c.id ===
        next.soldier_groups.find((g) => g.id === survivor?.group_id)
          ?.origin_castle_id,
    );
    const now = next.soldier_groups.find((g) => g.id === survivor?.group_id);
    const move = moveOf(plan, survivor?.group_id as number);
    expect(move.keys.map((k) => k.t)).toEqual([0, clash?.time as number, 1]);
    expect(distance(move.keys[0] as never, origin as never)).toBeCloseTo(
      toMapDistance(CASTLE_RADIUS),
      6,
    );
    expect(move.keys[1]).toMatchObject({ x: survivor?.x, y: survivor?.y });
    expect(move.keys[2]).toMatchObject({ x: now?.x, y: now?.y });
  });

  test("a castle's level change is a track at the start of the turn", () => {
    const upgraded = {
      ...fixtures["generated-map"].frames[0],
      castles: (fixtures["generated-map"].frames[0] as Generation).castles.map(
        (castle, index) =>
          index === 0
            ? { ...castle, upgrades: { speed: 2, clash_stop_generations: 1 } }
            : castle,
      ),
    } as Generation;
    const plan = planTurn(
      fixtures["generated-map"].frames[0] as Generation,
      upgraded,
    );
    expect(tracksOf(plan, "castleLevel")).toEqual([
      {
        kind: "castleLevel",
        castleId: upgraded.castles[0]?.id as number,
        t: 0,
        from: 1,
        to: 2,
      },
    ]);
    expect(
      sampleTurn(plan, 0).castles.get(upgraded.castles[0]?.id as number)?.level,
    ).toBe(1);
    expect(
      sampleTurn(plan, 0.01).castles.get(upgraded.castles[0]?.id as number)
        ?.level,
    ).toBe(2);
  });
});

describe("planTurn on every fixture pair", () => {
  for (const { name, index, previous, next } of pairs) {
    const plan = planTurn(previous, next);

    test(`${name}[${index}]: at t = 1 everything equals the next generation exactly`, () => {
      const sample = sampleTurn(plan, 1);
      for (const group of next.soldier_groups) {
        expect(sample.groups.get(group.id)).toMatchObject({
          x: group.x,
          y: group.y,
          count: group.soldiers,
          visible: true,
          sink: 0,
          scale: 1,
        });
      }
      for (const [id, pose] of sample.groups) {
        if (!next.soldier_groups.some((g) => g.id === id)) {
          expect(pose.visible).toBe(false);
        }
      }
      for (const castle of next.castles) {
        expect(sample.castles.get(castle.id)).toEqual({
          count: castle.soldiers,
          team: castle.team_color,
          level: castleDisplayLevel(castle.upgrades),
        });
      }
    });

    test(`${name}[${index}]: at t = 0 it shows the previous generation`, () => {
      const sample = sampleTurn(plan, 0);
      for (const group of previous.soldier_groups) {
        expect(sample.groups.get(group.id)).toMatchObject({
          x: group.x,
          y: group.y,
          count: group.soldiers,
          visible: true,
        });
      }
      for (const castle of previous.castles) {
        const pose = sample.castles.get(castle.id);
        expect(pose?.team).toBe(castle.team_color);
        expect(pose?.level).toBe(castleDisplayLevel(castle.upgrades));
        // The soldiers that leave the castle or pay for upgrades are gone from t = 0 on, but the
        // count has not ticked yet at the very start.
        expect(pose?.count).toBe(castle.soldiers);
      }
    });

    test(`${name}[${index}]: the turn's arithmetic reproduces every castle's count`, () => {
      // Each castle's track must come from the replayed rules, not the straight-line fallback.
      for (const track of tracksOf(plan, "castleCount")) {
        const keys = track.keys;
        const castle = next.castles.find((c) => c.id === track.castleId);
        expect(keys.at(-1)).toEqual({
          t: 1,
          value: castle?.soldiers as number,
        });
        expect([track.castleId, track.replayed]).toEqual([
          track.castleId,
          true,
        ]);
      }
    });

    test(`${name}[${index}]: keys are in time order and inside the turn`, () => {
      for (const track of plan.tracks) {
        const keys =
          track.kind === "move" ||
          track.kind === "groupCount" ||
          track.kind === "castleCount" ||
          track.kind === "castleTeam"
            ? track.keys
            : [];
        for (let k = 1; k < keys.length; k++) {
          expect((keys[k] as { t: number }).t).toBeGreaterThanOrEqual(
            (keys[k - 1] as { t: number }).t,
          );
        }
        for (const key of keys) {
          expect(key.t).toBeGreaterThanOrEqual(0);
          expect(key.t).toBeLessThanOrEqual(1);
        }
      }
    });

    test(`${name}[${index}]: no group jumps within the turn`, () => {
      // A group may not move farther between two samples than its speed allows, except the
      // short slide to a stop at a balanced clash.
      let last = sampleTurn(plan, 0);
      for (let step = 1; step <= 200; step++) {
        const sample = sampleTurn(plan, step / 200);
        for (const [id, pose] of sample.groups) {
          const old = last.groups.get(id);
          if (old?.visible && pose.visible) {
            expect(distance(old, pose)).toBeLessThan(0.1);
          }
        }
        last = sample;
      }
    });
  }

  test("consecutive turns join up: a turn ends where the next one starts", () => {
    for (const [name, demo] of Object.entries(fixtures)) {
      for (let index = 2; index < demo.frames.length; index++) {
        const earlier = planTurn(
          demo.frames[index - 2] as Generation,
          demo.frames[index - 1] as Generation,
        );
        const later = planTurn(
          demo.frames[index - 1] as Generation,
          demo.frames[index] as Generation,
        );
        const end = sampleTurn(earlier, 1);
        const start = sampleTurn(later, 0);
        for (const [id, pose] of start.groups) {
          const before = end.groups.get(id);
          if (before?.visible) {
            expect([name, id, before.x, before.y, before.count]).toEqual([
              name,
              id,
              pose.x,
              pose.y,
              pose.count,
            ]);
          }
        }
        for (const [id, pose] of start.castles) {
          expect([name, id, end.castles.get(id)]).toEqual([name, id, pose]);
        }
      }
    }
  });

  test("castle counts follow the turn's rules rather than a straight line", () => {
    // Every castle's track ticks down at t = 0 when it sends a group.
    const { previous, next, plan } = pair("six-teams", 1);
    const sender = next.soldier_groups.find(
      (group) => !previous.soldier_groups.some((g) => g.id === group.id),
    );
    const count = tracksOf(plan, "castleCount").find(
      (t) => t.castleId === sender?.origin_castle_id,
    );
    expect(count?.keys.map((k) => k.t).slice(0, 2)).toEqual([0, 0.06]);
    expect(count?.keys[1]?.value).toBeLessThan(count?.keys[0]?.value as number);
  });
});

describe("speed", () => {
  test("speed follows the upgrade table", () => {
    expect(speedOf({})).toBe(0.5);
    expect(speedOf({ speed: 3 })).toBe(1.5);
    expect(speedOf({ speed: 9 })).toBe(2);
  });
});
