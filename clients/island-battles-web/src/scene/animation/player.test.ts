import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../../api/generated/game";
import type { TurnPlan, TurnSample } from "./plan";
import { type Stage, TurnPlayer } from "./player";

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

const FRAME_MS = 1000 / 60;

/** A stage that remembers what it was told, and the poses it was shown. */
class FakeStage implements Stage {
  calls: string[] = [];
  settled: Generation | null = null;
  /** True between begin and end: a turn is on stage. */
  midTurn = false;
  lastSample: TurnSample | null = null;
  /** Every group's position at each apply, for jump checks. */
  jumps: number[] = [];
  private lastPoses = new Map<number, { x: number; y: number }>();

  settle(generation: Generation): void {
    this.calls.push(`settle:${generation.id}`);
    this.settled = generation;
    this.midTurn = false;
    this.lastSample = null;
    this.lastPoses.clear();
    for (const group of generation.soldier_groups) {
      this.lastPoses.set(group.id, { x: group.x, y: group.y });
    }
  }
  begin(plan: TurnPlan): void {
    this.calls.push(`begin:${plan.next.id}`);
    this.midTurn = true;
  }
  apply(sample: TurnSample): void {
    this.lastSample = sample;
    for (const [id, pose] of sample.groups) {
      const before = this.lastPoses.get(id);
      if (before && pose.visible) {
        this.jumps.push(Math.hypot(pose.x - before.x, pose.y - before.y));
      }
      if (pose.visible) this.lastPoses.set(id, { x: pose.x, y: pose.y });
    }
  }
  end(plan: TurnPlan): void {
    this.calls.push(`end:${plan.next.id}`);
    this.midTurn = false;
    this.settled = plan.next;
    this.lastPoses.clear();
    for (const group of plan.next.soldier_groups) {
      this.lastPoses.set(group.id, { x: group.x, y: group.y });
    }
  }
}

/** Runs `ms` of fake time in frames; returns the time at which the player went idle, if it did. */
function run(player: TurnPlayer, ms: number, onFrame?: (time: number) => void) {
  for (let time = 0; time < ms; time += FRAME_MS) {
    player.tick(FRAME_MS);
    onFrame?.(time);
  }
}

const busy = load("busy-turn");

describe("TurnPlayer", () => {
  test("plays a whole fixture with no jumps between frames or turns", () => {
    for (const name of ["busy-turn", "six-teams", "generated-map"]) {
      const demo = load(name);
      const stage = new FakeStage();
      const player = new TurnPlayer(stage);
      const frames = demo.frames;
      player.seek(frames[0] as Generation);
      for (let index = 1; index < frames.length; index++) {
        player.play(
          frames[index - 1] as Generation,
          frames[index] as Generation,
        );
        run(player, 1500 + 100);
      }
      expect(player.busy).toBe(false);
      expect(stage.settled).toBe(frames.at(-1) as Generation);
      // Settled once, up front: the turns joined up on their own.
      expect(
        stage.calls.filter((call) => call.startsWith("settle")),
      ).toHaveLength(1);
      expect(stage.calls.filter((call) => call.startsWith("end"))).toHaveLength(
        frames.length - 1,
      );
      // At 60 frames a second a group moves at most 2 units a turn over 90 frames, plus the
      // short slide a stopped survivor makes; nothing may leap.
      expect(Math.max(...stage.jumps)).toBeLessThan(0.1);
    }
  });

  test("a turn takes the configured time at normal speed", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage, { turnMs: 1000 });
    const [first, second] = busy.frames as [Generation, Generation];
    player.seek(first);
    player.play(first, second);
    run(player, 900);
    expect(stage.midTurn).toBe(true);
    run(player, 200);
    expect(stage.midTurn).toBe(false);
    expect(stage.settled).toBe(second);
  });

  test("setSpeed scales the pace and zero pauses", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage, { turnMs: 1000 });
    const [first, second] = busy.frames as [Generation, Generation];
    player.seek(first);
    player.setSpeed(0);
    player.play(first, second);
    run(player, 5000);
    expect(stage.midTurn).toBe(false);
    expect(player.busy).toBe(true);
    player.setSpeed(2);
    run(player, 520);
    expect(stage.settled).toBe(second);
  });

  test("plays about 1x with nothing waiting and up to 4x as the queue grows", () => {
    const player = new TurnPlayer(new FakeStage());
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    expect(player.effectiveSpeed).toBe(1);
    player.play(frames[0] as Generation, frames[1] as Generation);
    player.play(frames[1] as Generation, frames[2] as Generation);
    expect(player.effectiveSpeed).toBe(3);
    player.play(frames[2] as Generation, frames[3] as Generation);
    expect(player.effectiveSpeed).toBe(4);
  });

  test("turns that arrive together catch up within a few seconds", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    // Three bots' worth of turns land at once.
    for (let index = 1; index <= 3; index++) {
      player.play(frames[index - 1] as Generation, frames[index] as Generation);
    }
    let finished = Number.POSITIVE_INFINITY;
    run(player, 6000, (time) => {
      if (!player.busy && finished === Number.POSITIVE_INFINITY)
        finished = time;
    });
    expect(stage.settled).toBe(frames[3] as Generation);
    expect(finished).toBeLessThan(3000);
    // They were all played, none skipped.
    expect(stage.calls.filter((call) => call.startsWith("end"))).toHaveLength(
      3,
    );
  });

  test("a viewer never trails a fast live game by more than a few seconds", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    // One turn arrives every 250 ms, six times faster than they play at 1x.
    const arrivedAt = new Map<number, number>();
    const shownAt = new Map<number, number>();
    let next = 1;
    let time = 0;
    for (; time < 12_000; time += FRAME_MS) {
      if (next < frames.length && time >= next * 250) {
        arrivedAt.set((frames[next] as Generation).id, time);
        player.play(frames[next - 1] as Generation, frames[next] as Generation);
        next++;
      }
      const before = stage.settled;
      player.tick(FRAME_MS);
      if (stage.settled !== before && stage.settled) {
        shownAt.set(stage.settled.id, time);
      }
    }
    expect(stage.settled).toBe(frames.at(-1) as Generation);
    for (const [id, shown] of shownAt) {
      const arrived = arrivedAt.get(id);
      if (arrived !== undefined) expect(shown - arrived).toBeLessThan(3000);
    }
  });

  test("beyond a few turns behind, it skips to the newest", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    for (let index = 1; index <= 4; index++) {
      player.play(frames[index - 1] as Generation, frames[index] as Generation);
    }
    expect(player.waiting).toBe(1);
    run(player, 2000);
    expect(stage.settled).toBe(frames[4] as Generation);
    // Only the newest turn played; the rest were skipped.
    expect(stage.calls.filter((call) => call.startsWith("end:"))).toEqual([
      `end:${(frames[4] as Generation).id}`,
    ]);
    expect(stage.calls).toContain(`settle:${(frames[3] as Generation).id}`);
  });

  test("seek ends a turn in progress at once and leaves nothing half-way", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    player.play(frames[0] as Generation, frames[1] as Generation);
    player.play(frames[1] as Generation, frames[2] as Generation);
    run(player, 700);
    expect(stage.midTurn).toBe(true);
    expect(player.progress).not.toBeNull();

    player.seek(frames[5] as Generation);
    expect(stage.midTurn).toBe(false);
    expect(stage.settled).toBe(frames[5] as Generation);
    expect(stage.calls.at(-1)).toBe(`settle:${(frames[5] as Generation).id}`);
    expect(player.busy).toBe(false);
    expect(player.progress).toBeNull();
    // Nothing else happens afterwards: the queue was dropped.
    const calls = stage.calls.length;
    run(player, 3000);
    expect(stage.calls).toHaveLength(calls);
  });

  test("a turn that does not follow what is shown settles on its start first", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    player.play(frames[4] as Generation, frames[5] as Generation);
    run(player, 100);
    expect(stage.calls).toContain(`settle:${(frames[4] as Generation).id}`);
    expect(
      stage.calls.indexOf(`settle:${(frames[4] as Generation).id}`),
    ).toBeLessThan(
      stage.calls.indexOf(`begin:${(frames[5] as Generation).id}`),
    );
  });

  test("reports each generation as it is shown", () => {
    const shown: number[] = [];
    const player = new TurnPlayer(new FakeStage(), {
      onShown: (generation) => shown.push(generation.id),
    });
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    player.play(frames[0] as Generation, frames[1] as Generation);
    run(player, 2000);
    expect(shown).toEqual([
      (frames[0] as Generation).id,
      (frames[1] as Generation).id,
    ]);
  });
});
