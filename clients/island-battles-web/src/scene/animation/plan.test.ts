import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../../api/generated/game";
import { castleDisplayLevel } from "../castleLevel";
import {
  groupLook,
  insideOutline,
  insideWaterline,
  sharesLandmass,
  shorePoint,
} from "../shore";
import {
  clashStops,
  HELD_RATTLE_SPAN,
  isHeld,
  type MoveTrack,
  planTurn,
  sampleTurn,
  speedOf,
  stoppedByClashAt,
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

/** How a group of `frame` is drawn at rest: its boat, its build and its squad (shore.ts). */
function drawnIn(
  frame: Generation,
  group: Generation["soldier_groups"][number],
) {
  const castle = (id: number) => frame.castles.find((c) => c.id === id);
  return groupLook(
    group,
    castle(group.origin_castle_id),
    castle(group.destination_castle_id),
  );
}

const distance = (
  left: { x: number; y: number },
  right: { x: number; y: number },
) => Math.hypot(left.x - right.x, left.y - right.y);

describe("planTurn on fixture pairs", () => {
  test("a launch walks out of the castle while its boat is built at the shore, then sails", () => {
    // busy-turn frame 4: a group leaves its castle and nothing clashes.
    const { previous, next, plan } = pair("busy-turn", 4);
    const launched = next.soldier_groups.filter(
      (group) => !previous.soldier_groups.some((old) => old.id === group.id),
    );
    expect(launched.length).toBeGreaterThan(0);
    for (const group of launched) {
      const origin = next.castles.find((c) => c.id === group.origin_castle_id);
      const destination = next.castles.find(
        (c) => c.id === group.destination_castle_id,
      );
      const move = moveOf(plan, group.id);
      // The server sends a group from its castle's centre, and so does the plan.
      expect(move.keys[0]).toMatchObject({ t: 0, x: origin?.x, y: origin?.y });
      expect(move.keys.at(-1)).toMatchObject({ t: 1, x: group.x, y: group.y });
      const shore = shorePoint(origin as never, destination as never);
      // At the start the boat is not built, and the squad is at the gate, still coming out.
      const start = sampleTurn(plan, 0).groups.get(group.id);
      expect(start).toMatchObject({ x: shore.x, y: shore.y, build: 0 });
      expect(start?.squad?.scale).toBe(0);
      let build = 0;
      let reach = 0;
      for (let step = 0; step <= 40; step++) {
        const pose = sampleTurn(plan, step / 40).groups.get(group.id);
        if (!pose) throw new Error("no pose");
        // The boat waits at the shore point, outside the waterline, until it sails.
        expect(distance(pose, origin as never)).toBeGreaterThanOrEqual(
          distance(shore, origin as never) - 1e-9,
        );
        expect(insideWaterline(origin as never, pose)).toBe(false);
        // It is built up, never torn down; once it is complete the squad has boarded.
        expect(pose.build).toBeGreaterThanOrEqual(build);
        build = pose.build;
        if (pose.squad) {
          const walked = distance(pose.squad, origin as never);
          expect(walked).toBeGreaterThanOrEqual(reach - 1e-9);
          reach = walked;
          if (pose.squad.scale > 0 && pose.build < 1) {
            expect(insideOutline(origin as never, pose.squad)).toBe(true);
          }
        }
      }
      // No spawn: the boat grows as it is built.
      expect(
        tracksOf(plan, "spawn").some((spawn) => spawn.groupId === group.id),
      ).toBe(false);
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

  test("an arrival docks at the shore and its squad walks on into the castle, which counts it", () => {
    // busy-turn frame 2: a group arrives and nothing clashes.
    const { previous, next, plan } = pair("busy-turn", 2);
    const arriving = previous.soldier_groups.filter(
      (group) => !next.soldier_groups.some((now) => now.id === group.id),
    );
    expect(arriving).toHaveLength(1);
    for (const group of arriving) {
      const castle = next.castles.find(
        (c) => c.id === group.destination_castle_id,
      );
      const origin = previous.castles.find(
        (c) => c.id === group.origin_castle_id,
      );
      const move = moveOf(plan, group.id);
      expect(move.keys[0]).toMatchObject({ t: 0, x: group.x, y: group.y });
      const merge = tracksOf(plan, "merge").find(
        (track) => track.groupId === group.id,
      );
      expect(merge?.castleId).toBe(castle?.id);
      // The true position goes on to the castle's centre at the group's speed, and lands there.
      const last = move.keys.at(-1);
      expect(last).toMatchObject({ x: castle?.x, y: castle?.y });
      expect(last?.t).toBe(merge?.t1 as number);
      expect(merge?.t0).toBe(merge?.t1 as number);
      expect(last?.t).toBeCloseTo(
        distance(group, castle as never) / speedOf(group.upgrades),
        9,
      );
      // The boat stops at the shore and shrinks away there; the squad walks on to the gate.
      const shore = shorePoint(castle as never, origin as never);
      expect(insideWaterline(castle as never, shore)).toBe(false);
      let docked = false;
      let left = Number.POSITIVE_INFINITY;
      for (let step = 0; step < 40; step++) {
        const time = (step / 40) * (last?.t as number);
        const pose = sampleTurn(plan, time).groups.get(group.id);
        if (!pose) throw new Error("no pose");
        expect(insideWaterline(castle as never, pose)).toBe(false);
        if (pose.squad) {
          docked = true;
          expect(pose).toMatchObject({ x: shore.x, y: shore.y });
          const away = distance(pose.squad, castle as never);
          expect(away).toBeLessThanOrEqual(left + 1e-9);
          left = away;
        } else {
          // Once docked the group stays on land until it lands.
          expect(docked).toBe(false);
        }
      }
      expect(docked).toBe(true);
      // Shrunk away before it lands.
      expect(
        sampleTurn(plan, (last?.t as number) - 1e-6).groups.get(group.id)
          ?.scale,
      ).toBe(0);
      // Gone from the board once landed, and the sample before that still shows it.
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
    // busy-turn frame 7: a group arrives and takes a castle.
    const { previous, next, plan } = pair("busy-turn", 7);
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
    const destination = next.castles.find(
      (c) => c.id === now?.destination_castle_id,
    );
    expect(move.keys[0]).toMatchObject({ x: origin?.x, y: origin?.y });
    expect(sampleTurn(plan, 0).groups.get(now?.id as number)).toMatchObject(
      shorePoint(origin as never, destination as never),
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

describe("rattle (#169)", () => {
  const rattles = (plan: TurnPlan) => tracksOf(plan, "rattle");

  test("the survivor of a balanced clash rattles from the clash to the end of the turn", () => {
    // generated-map frame 4: 10 against 11, a balanced clash at t = 0.875.
    const { next, plan } = pair("generated-map", 4);
    const clash = next.clashes[0];
    expect(clash && clashStops(clash)).toBe(true);
    const survivor = clash?.participants.find((p) => p.soldiers_after > 0);
    expect(rattles(plan)).toEqual([
      {
        kind: "rattle",
        groupId: survivor?.group_id as number,
        t0: clash?.time as number,
        t1: 1,
      },
    ]);
    const id = survivor?.group_id as number;
    expect(
      sampleTurn(plan, (clash?.time ?? 0) - 0.01).groups.get(id)?.rattle,
    ).toBe(0);
    expect(sampleTurn(plan, 0.9375).groups.get(id)?.rattle).toBeCloseTo(0.5, 6);
    expect(sampleTurn(plan, 1).groups.get(id)?.rattle).toBe(1);
  });

  test("a boat that is still held rattles in a burst at the start of the turn", () => {
    // generated-map frame 5: the same boat, no clash this turn.
    const { previous, next, plan } = pair("generated-map", 5);
    const held = next.soldier_groups.filter((g) =>
      isHeld(previous, next, g.id),
    );
    expect(held).toHaveLength(1);
    const id = held[0]?.id as number;
    expect(rattles(plan)).toEqual([
      { kind: "rattle", groupId: id, t0: 0, t1: HELD_RATTLE_SPAN },
    ]);
    expect(sampleTurn(plan, 0).groups.get(id)?.rattle).toBe(0);
    expect(sampleTurn(plan, 0.15).groups.get(id)?.rattle).toBeCloseTo(0.5, 6);
    expect(sampleTurn(plan, 0.5).groups.get(id)?.rattle).toBe(1);
  });

  test("a defeated group sinks and does not rattle", () => {
    const { next, plan } = pair("generated-map", 4);
    const lost = next.clashes[0]?.participants.find(
      (p) => p.soldiers_after === 0,
    );
    expect(tracksOf(plan, "sink").map((t) => t.groupId)).toContain(
      lost?.group_id as number,
    );
    expect(rattles(plan).map((t) => t.groupId)).not.toContain(lost?.group_id);
    expect(stoppedByClashAt(next, lost?.group_id as number)).toBeUndefined();
  });

  test("a clash that is not balanced stops nobody: the bigger group sails on and does not rattle", () => {
    // busy-turn frame 1, clash 1: 10 against 4.
    const { next, plan } = pair("busy-turn", 1);
    const clash = next.clashes.find((c) => !clashStops(c));
    expect(clash).toBeDefined();
    const winner = clash?.participants.find((p) => p.soldiers_after > 0);
    expect(rattles(plan).map((t) => t.groupId)).not.toContain(winner?.group_id);
    expect(stoppedByClashAt(next, winner?.group_id as number)).toBeUndefined();
  });

  test("only groups that a clash stopped or still holds rattle, in every fixture pair", () => {
    for (const { previous, next, plan } of pairs.map((entry) => ({
      ...entry,
      plan: planTurn(entry.previous, entry.next),
    }))) {
      for (const track of rattles(plan)) {
        const stopped = stoppedByClashAt(next, track.groupId);
        if (stopped !== undefined) {
          expect([track.t0, track.t1]).toEqual([stopped, 1]);
        } else {
          expect(isHeld(previous, next, track.groupId)).toBe(true);
          expect([track.t0, track.t1]).toEqual([0, HELD_RATTLE_SPAN]);
        }
      }
      // A group that moved from one position to another never rattles.
      for (const group of next.soldier_groups) {
        const before = previous.soldier_groups.find((g) => g.id === group.id);
        const moved = before && (before.x !== group.x || before.y !== group.y);
        if (moved && stoppedByClashAt(next, group.id) === undefined) {
          expect(rattles(plan).map((t) => t.groupId)).not.toContain(group.id);
        }
      }
    }
  });

  test("a group that has arrived at its castle is not held", () => {
    const { previous, next } = pair("generated-map", 5);
    const group = next.soldier_groups[0];
    expect(group).toBeDefined();
    const castle = next.castles.find(
      (c) => c.id === group?.destination_castle_id,
    );
    const there = {
      ...next,
      soldier_groups: [{ ...(group as object), x: castle?.x, y: castle?.y }],
    } as Generation;
    const was = {
      ...previous,
      soldier_groups: [{ ...(group as object), x: castle?.x, y: castle?.y }],
    } as Generation;
    expect(isHeld(was, there, group?.id as number)).toBe(false);
  });
});

describe("planTurn on every fixture pair", () => {
  for (const { name, index, previous, next } of pairs) {
    const plan = planTurn(previous, next);

    test(`${name}[${index}]: at t = 1 everything equals the next generation exactly`, () => {
      const sample = sampleTurn(plan, 1);
      for (const group of next.soldier_groups) {
        const look = drawnIn(next, group);
        const pose = sample.groups.get(group.id);
        expect(pose).toMatchObject({
          x: look.x,
          y: look.y,
          build: look.build,
          scale: look.boatScale,
          count: group.soldiers,
          visible: true,
          sink: 0,
        });
        expect(pose?.squad).toEqual(look.squad);
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
        const look = drawnIn(previous, group);
        const pose = sample.groups.get(group.id);
        expect(pose).toMatchObject({
          x: look.x,
          y: look.y,
          build: look.build,
          scale: look.boatScale,
          count: group.soldiers,
          visible: true,
        });
        expect(pose?.squad).toEqual(look.squad);
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

describe("castle counts and growth", () => {
  const castle = (
    id: number,
    team: number,
    soldiers: number,
    x: number,
  ): Generation["castles"][number] => ({
    id,
    team_color: team,
    soldiers,
    x,
    y: 0,
    upgrades: { speed: 1, clash_stop_generations: 1 },
  });
  const group = (): Generation["soldier_groups"][number] => ({
    id: 5,
    team_color: 1,
    soldiers: 26,
    x: 3.9,
    y: 0,
    origin_castle_id: 0,
    destination_castle_id: 1,
    clash_radius: 0.25,
    upgrades: { speed: 1, clash_stop_generations: 1 },
  });
  const countOf = (plan: TurnPlan, castleId: number) => {
    const track = tracksOf(plan, "castleCount").find(
      (t) => t.castleId === castleId,
    );
    if (!track) throw new Error(`no count track for castle ${castleId}`);
    return track;
  };

  test("a team's castle grows, a neutral one keeps its soldiers, and both replay exactly", () => {
    const previous: Generation = {
      id: 1,
      castles: [castle(0, 1, 30, 0), castle(1, 0, 20, 4)],
      soldier_groups: [],
      clashes: [],
    };
    const next: Generation = {
      id: 2,
      castles: [castle(0, 1, 31, 0), castle(1, 0, 20, 4)],
      soldier_groups: [],
      clashes: [],
    };
    const plan = planTurn(previous, next);
    expect(countOf(plan, 0).replayed).toBe(true);
    expect(countOf(plan, 0).keys.map((key) => key.value)).toContain(31);
    expect(countOf(plan, 1).replayed).toBe(true);
    expect(new Set(countOf(plan, 1).keys.map((key) => key.value))).toEqual(
      new Set([20]),
    );
    // A server that still grew the neutral castle would not match the replay.
    const grown = planTurn(previous, {
      ...next,
      castles: [castle(0, 1, 31, 0), castle(1, 0, 21, 4)],
    });
    expect(countOf(grown, 1).replayed).toBe(false);
  });

  test("a neutral castle captured this turn has the surplus without growth, then grows next turn", () => {
    const previous: Generation = {
      id: 1,
      castles: [castle(0, 1, 30, 0), castle(1, 0, 20, 4)],
      soldier_groups: [group()],
      clashes: [],
    };
    const captured: Generation = {
      id: 2,
      castles: [castle(0, 1, 31, 0), castle(1, 1, 6, 4)],
      soldier_groups: [],
      clashes: [],
    };
    const plan = planTurn(previous, captured);
    expect(countOf(plan, 1).replayed).toBe(true);
    expect(sampleTurn(plan, 1).castles.get(1)).toMatchObject({
      team: 1,
      count: 6,
    });
    // One soldier more would mean the neutral castle grew before the landing.
    const grew = planTurn(previous, {
      ...captured,
      castles: [castle(0, 1, 31, 0), castle(1, 1, 7, 4)],
    });
    expect(countOf(grew, 1).replayed).toBe(false);
    // The next turn it grows like any castle a team owns.
    const later = planTurn(captured, {
      id: 3,
      castles: [castle(0, 1, 32, 0), castle(1, 1, 7, 4)],
      soldier_groups: [],
      clashes: [],
    });
    expect(countOf(later, 1).replayed).toBe(true);
    expect(countOf(later, 1).keys.map((key) => key.value)).toContain(7);
  });
});

describe("shores", () => {
  test("no group of any sampled turn of any fixture is drawn inside its origin's or destination's waterline", () => {
    let boats = 0;
    for (const { name, index, previous, next } of pairs) {
      const plan = planTurn(previous, next);
      const castles = new Map(
        [...previous.castles, ...next.castles].map((c) => [c.id, c]),
      );
      const routes = new Map(
        [
          ...previous.soldier_groups,
          ...plan.transients,
          ...next.soldier_groups,
        ].map((group) => [group.id, group]),
      );
      for (let step = 0; step <= 40; step++) {
        const sample = sampleTurn(plan, step / 40);
        for (const [id, pose] of sample.groups) {
          const group = routes.get(id);
          if (!group || !pose.visible) continue;
          const origin = castles.get(group.origin_castle_id);
          const destination = castles.get(group.destination_castle_id);
          // Between castles of one landmass the boat crosses the bridge by design.
          if (origin && destination && sharesLandmass(origin, destination)) {
            continue;
          }
          boats++;
          for (const castle of [origin, destination]) {
            if (castle && insideWaterline(castle, pose)) {
              throw new Error(
                `${name}[${index}] t=${step / 40}: group ${id} is drawn inside castle ${castle.id}'s waterline`,
              );
            }
          }
        }
      }
    }
    expect(boats).toBeGreaterThan(1000);
  });

  test("the settled position of every group of every frame is outside both waterlines", () => {
    for (const demo of Object.values(fixtures)) {
      for (const frame of demo.frames) {
        for (const group of frame.soldier_groups) {
          const origin = frame.castles.find(
            (c) => c.id === group.origin_castle_id,
          );
          const destination = frame.castles.find(
            (c) => c.id === group.destination_castle_id,
          );
          if (origin && destination && sharesLandmass(origin, destination)) {
            continue;
          }
          const drawn = drawnIn(frame, group);
          for (const castle of [origin, destination]) {
            if (castle) expect(insideWaterline(castle, drawn)).toBe(false);
          }
        }
      }
    }
  });

  test("a boat in open sea is sampled at its true position", () => {
    let seen = 0;
    for (const { previous, next } of pairs) {
      if (next.clashes.length > 0) continue;
      const plan = planTurn(previous, next);
      for (const group of next.soldier_groups) {
        const before = previous.soldier_groups.find((g) => g.id === group.id);
        const ends = next.castles.filter(
          (c) =>
            c.id === group.origin_castle_id ||
            c.id === group.destination_castle_id,
        );
        if (
          !before ||
          ends.some(
            (castle) =>
              distance(castle, group) < 2.5 || distance(castle, before) < 2.5,
          )
        ) {
          continue;
        }
        seen++;
        expect(sampleTurn(plan, 1).groups.get(group.id)).toMatchObject({
          x: group.x,
          y: group.y,
        });
        expect(sampleTurn(plan, 0).groups.get(group.id)).toMatchObject({
          x: before.x,
          y: before.y,
        });
      }
    }
    expect(seen).toBeGreaterThan(0);
  });
});

describe("speed", () => {
  test("speed follows the upgrade table", () => {
    expect(speedOf({})).toBe(0.5);
    expect(speedOf({ speed: 3 })).toBe(1.5);
    expect(speedOf({ speed: 9 })).toBe(2);
  });
});
