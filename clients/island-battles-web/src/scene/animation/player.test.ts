import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../../api/generated/game";
import { drawnPosition } from "../shore";
import type { TurnPlan, TurnSample } from "./plan";
import {
  DEFAULT_MAX_BEHIND,
  DEFAULT_MAX_FRAME_MS,
  type LiveTiming,
  MAX_LIVE_TURN_MS,
  MIN_LIVE_TURN_MS,
  PLAYING,
  type Stage,
  TurnPlayer,
  WATCHING,
} from "./player";

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

/** Where the scene draws a group of `generation`: at its island's shore near a castle. */
function drawnIn(
  generation: Generation,
  group: Generation["soldier_groups"][number],
) {
  const castle = (id: number) => generation.castles.find((c) => c.id === id);
  return drawnPosition(
    group,
    castle(group.origin_castle_id),
    castle(group.destination_castle_id),
  );
}

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
      this.lastPoses.set(group.id, drawnIn(generation, group));
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
      this.lastPoses.set(group.id, drawnIn(plan.next, group));
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

  test("a queue raises the speed gradually, never in steps, up to 4x (#176)", () => {
    const player = new TurnPlayer(new FakeStage());
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    expect(player.effectiveSpeed).toBe(1);
    for (let index = 1; index <= 8; index++) {
      player.play(frames[index - 1] as Generation, frames[index] as Generation);
    }
    // Queuing changes nothing at once.
    expect(player.effectiveSpeed).toBe(1);
    const speeds: number[] = [];
    run(player, 1500, () => speeds.push(player.effectiveSpeed));
    // It rises over a few hundred milliseconds: by less than a fifth a frame, ...
    for (let index = 1; index < speeds.length; index++) {
      expect(
        Math.abs((speeds[index] as number) - (speeds[index - 1] as number)),
      ).toBeLessThan(0.2);
    }
    // (about 100 ms in)
    expect(speeds[5] as number).toBeLessThan(2.5);
    // ... and reaches the catch-up the backlog asks for, at most 4x.
    expect(Math.max(...speeds)).toBeGreaterThan(3);
    expect(Math.max(...speeds)).toBeLessThanOrEqual(4);
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
    let now = 0;
    const player = new TurnPlayer(stage, { now: () => now });
    player.setLive(true);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    // One turn arrives every 250 ms, six times faster than they play at 1x.
    const arrivedAt = new Map<number, number>();
    const shownAt = new Map<number, number>();
    let next = 1;
    let time = 0;
    for (; time < 12_000; time += FRAME_MS) {
      now = time;
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

  test("beyond a large backlog, it skips to the newest", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage, { maxBehind: 3 });
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

  test("by default the backlog it skips at is ten turns", () => {
    const player = new TurnPlayer(new FakeStage());
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    for (let index = 1; index <= DEFAULT_MAX_BEHIND; index++) {
      const previous = frames[(index - 1) % frames.length] as Generation;
      player.play(previous, frames[index % frames.length] as Generation);
    }
    expect(player.waiting).toBe(DEFAULT_MAX_BEHIND);
    player.play(frames[0] as Generation, frames[1] as Generation);
    expect(player.waiting).toBe(1);
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

/** Many generations in a row, from the busy fixture's, numbered 0, 1, 2, ... */
const long: Generation[] = Array.from({ length: 400 }, (_, index) => ({
  ...(busy.frames[index % busy.frames.length] as Generation),
  id: index,
}));

/** A stage that records the turn position shown: the generation's number plus the progress. */
class PositionStage implements Stage {
  position = 0;
  private current = 0;
  settle(generation: Generation): void {
    this.position = generation.id;
  }
  begin(plan: TurnPlan): void {
    this.current = plan.previous.id;
  }
  apply(sample: TurnSample): void {
    this.position = this.current + sample.t;
  }
  end(plan: TurnPlan): void {
    this.position = plan.next.id;
  }
}

/**
 * Follows a live game for `ms` whose turns arrive `gap(n)` after the one before, in frames of
 * `frameMs`. Records, over the second half, how fast the turns play (turns a second, which a
 * cruising boat's speed is proportional to) and the turn length the player chose.
 */
function follow(
  gap: (index: number) => number,
  ms: number,
  frameMs = FRAME_MS,
) {
  const stage = new PositionStage();
  let now = 0;
  const player = new TurnPlayer(stage, { now: () => now });
  player.setLive(true);
  player.seek(long[0] as Generation);
  let next = 0;
  let arrival = 500;
  const rates: number[] = [];
  const durations: number[] = [];
  let last = stage.position;
  while (now < ms) {
    now += frameMs;
    while (now >= arrival && next < long.length - 1) {
      player.play(long[next] as Generation, long[next + 1] as Generation);
      next++;
      arrival += gap(next);
    }
    player.tick(frameMs);
    if (now > ms / 2) {
      rates.push(((stage.position - last) * 1000) / frameMs);
      durations.push(player.turnDuration);
    }
    last = stage.position;
  }
  return { rates, durations };
}

/** The largest change in speed from one frame to the next, as a fraction of the mean. */
function largestChange(rates: readonly number[]): number {
  const mean = rates.reduce((sum, rate) => sum + rate, 0) / rates.length;
  let largest = 0;
  for (let index = 1; index < rates.length; index++) {
    const change = Math.abs(
      (rates[index] as number) - (rates[index - 1] as number),
    );
    largest = Math.max(largest, change / mean);
  }
  return largest;
}

describe("following live (#176)", () => {
  test("steady turns play over the time between them, at one steady speed", () => {
    const { rates, durations } = follow(() => 1000, 30_000);
    for (const duration of durations) {
      expect(Math.abs(duration - 1000)).toBeLessThan(10);
    }
    // Never stopped, and the same speed every frame, across every turn's end.
    expect(Math.min(...rates)).toBeGreaterThan(0.9);
    expect(largestChange(rates)).toBeLessThan(0.01);
  });

  test("slow turns play over at most 3 s, then wait", () => {
    const { rates, durations } = follow(() => 5000, 60_000);
    for (const duration of durations) {
      expect(duration).toBeCloseTo(MAX_LIVE_TURN_MS, 0);
    }
    const moving = rates.filter((rate) => rate > 1e-6);
    for (const rate of moving) expect(rate).toBeCloseTo(1000 / 3000, 2);
    // Three seconds of every five move.
    expect(moving.length / rates.length).toBeCloseTo(0.6, 1);
  });

  test("fast turns play over no less than 250 ms", () => {
    const { durations } = follow(() => 100, 10_000);
    for (const duration of durations) {
      expect(duration).toBeGreaterThanOrEqual(MIN_LIVE_TURN_MS);
      expect(duration).toBeLessThan(MIN_LIVE_TURN_MS + 5);
    }
  });

  test("turns that arrive in bursts play at a steady speed between the bursts", () => {
    // Three turns every three seconds: at once, or 80 ms apart as a live server sends them.
    for (const apart of [0, 80]) {
      const { rates, durations } = follow(
        (index) => (index % 3 === 0 ? 3000 - 2 * apart : apart),
        40_000,
      );
      for (const duration of durations) {
        expect(Math.abs(duration - 1000)).toBeLessThan(50);
      }
      expect(Math.min(...rates)).toBeGreaterThan(0.8);
      expect(largestChange(rates)).toBeLessThan(0.02);
    }
  });

  test("turns that arrive unevenly never stop the boats", () => {
    let seed = 1;
    const jitter = () => {
      seed = (seed * 1664525 + 1013904223) % 2 ** 32;
      return seed / 2 ** 32;
    };
    const { rates } = follow(() => 600 + 800 * jitter(), 60_000);
    expect(Math.min(...rates)).toBeGreaterThan(0);
    // The speed eases: no frame changes it by more than a few per cent.
    expect(largestChange(rates)).toBeLessThan(0.05);
  });
});

describe("time steps (#176)", () => {
  test("one long frame advances by at most 100 ms", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage, { turnMs: 1000 });
    const [first, second] = busy.frames as [Generation, Generation];
    player.seek(first);
    player.play(first, second);
    // A tab comes back after five seconds in the background.
    player.tick(5000);
    expect(player.progress).toBeCloseTo(DEFAULT_MAX_FRAME_MS / 1000, 6);
    expect(stage.midTurn).toBe(true);
  });

  test("no time is lost across a turn's end", () => {
    const stage = new PositionStage();
    let next = 0;
    const player = new TurnPlayer(stage, {
      turnMs: 1000,
      // As the game view and the dev page do: the next turn is queued as one ends.
      onIdle: () => {
        player.play(long[next] as Generation, long[next + 1] as Generation);
        next++;
      },
    });
    player.seek(long[0] as Generation);
    player.play(long[0] as Generation, long[1] as Generation);
    next = 1;
    // Frames of 17 ms do not divide a turn: every turn ends part-way through a frame.
    const frame = 17;
    for (let count = 1; count <= 600; count++) {
      player.tick(frame);
      expect(stage.position).toBeCloseTo((count * frame) / 1000, 9);
    }
  });
});

/** The positions `TurnPlayer` showed before its live timing became an option (#345). */
const watchingTrace = JSON.parse(
  readFileSync(new URL("./watching-trace.json", import.meta.url), "utf8"),
) as { steady: number[]; bursts: number[]; uneven: number[] };

interface LiveRun {
  /** The timing, or none for the player's default. */
  timing?: LiveTiming;
  frameMs?: number;
  /** No frame runs between these times, in ms, while turns keep arriving: a hidden tab. */
  hidden?: readonly [number, number];
  /** Switches the timing at this time, in ms. */
  switchTo?: { at: number; timing: LiveTiming };
  /** The game's turn pace, given to the player (`setLivePace`), as the game view does. */
  paceMs?: number;
  /** Jumps to the newest turn at this time, in ms, as Return to live does after a scrub back. */
  jumpAt?: number;
}

/**
 * Follows a live game for `ms` whose turns arrive `gap(n)` after the one before, the first at
 * 500 ms. Records the position every 10th frame; the lag as each turn arrives (the backlog: how
 * far the board is behind the generation that was open, at that turn's end); and, after the
 * first turn has played, each stop: the frames in a row in which the board did not move.
 */
function live(gap: (index: number) => number, ms: number, run: LiveRun = {}) {
  const frameMs = run.frameMs ?? FRAME_MS;
  const stage = new PositionStage();
  let now = 0;
  const player = new TurnPlayer(stage, {
    now: () => now,
    ...(run.timing ? { liveTiming: run.timing } : {}),
  });
  if (run.paceMs !== undefined) player.setLivePace(run.paceMs);
  player.setLive(true);
  player.seek(long[0] as Generation);
  let next = 0;
  let arrival = 500;
  let frame = 0;
  let switched = false;
  const trace: number[] = [];
  const lags: { at: number; index: number; lag: number }[] = [];
  let jumped = false;
  const stops: { at: number; frames: number }[] = [];
  let still = 0;
  let last = stage.position;
  const speeds: number[] = [];
  while (now < ms) {
    now += frameMs;
    while (now >= arrival && next < long.length - 1) {
      lags.push({
        at: now,
        index: next + 1,
        lag: player.waiting + (player.busy ? 1 - (player.progress ?? 0) : 0),
      });
      player.play(long[next] as Generation, long[next + 1] as Generation);
      next++;
      arrival += gap(next);
    }
    if (run.jumpAt !== undefined && !jumped && now >= run.jumpAt) {
      // Scrubbed back, then Return to live: following stops, and the newest turn is shown at once.
      player.setLive(false);
      player.seek(long[next] as Generation);
      player.setLive(true);
      jumped = true;
    }
    if (run.switchTo && !switched && now >= run.switchTo.at) {
      player.setLiveTiming(run.switchTo.timing);
      switched = true;
    }
    if (run.hidden && now >= run.hidden[0] && now < run.hidden[1]) continue;
    player.tick(frameMs);
    frame++;
    if (frame % 10 === 0) trace.push(stage.position);
    speeds.push(player.catchUpSpeed);
    if (stage.position >= 1) {
      if (stage.position === last) still++;
      else if (still > 0) {
        stops.push({ at: now, frames: still });
        still = 0;
      }
    }
    last = stage.position;
  }
  return { trace, lags, stops, speeds, player };
}

/** The lengths of the stops that ended from `from` ms on, in frames. */
const stopsFrom = (run: ReturnType<typeof live>, from: number) =>
  run.stops.filter((stop) => stop.at >= from).map((stop) => stop.frames);

/** The lags as the turns from `index` on arrive. */
const lagsFromTurn = (run: ReturnType<typeof live>, index: number) =>
  run.lags.filter((entry) => entry.index >= index).map((entry) => entry.lag);

/** The lags from `from` ms on. */
const lagsFrom = (run: ReturnType<typeof live>, from: number) =>
  run.lags.filter((entry) => entry.at >= from).map((entry) => entry.lag);

let seed = 1;
const uneven = () => {
  seed = (seed * 1664525 + 1013904223) % 2 ** 32;
  return 600 + 800 * (seed / 2 ** 32);
};

describe("live timing (#345)", () => {
  test("by default and with WATCHING it plays exactly as before the option", () => {
    const patterns: [keyof typeof watchingTrace, (index: number) => number][] =
      [
        ["steady", () => 1000],
        ["bursts", (index) => (index % 3 === 0 ? 3000 : 0)],
        ["uneven", () => uneven()],
      ];
    for (const timing of [undefined, WATCHING]) {
      for (const [name, gap] of patterns) {
        seed = 1;
        const { trace } = live(gap, 30_000, timing ? { timing } : {});
        const recorded = watchingTrace[name];
        expect(trace).toHaveLength(recorded.length);
        for (const [index, position] of trace.entries()) {
          expect(position).toBeCloseTo(recorded[index] as number, 9);
        }
      }
    }
  });

  test("a watcher stays about a turn behind at a 1 s pace", () => {
    const run = live(() => 1000, 60_000);
    for (const lag of lagsFrom(run, 20_000)) {
      expect(lag).toBeGreaterThan(1);
      expect(lag).toBeLessThan(1.1);
    }
  });

  test("a hand player's board reaches the open generation as its turn ends", () => {
    for (const pace of [500, 1000, 2000]) {
      const run = live(() => pace, 60_000, { timing: PLAYING });
      for (const lag of lagsFrom(run, 20_000)) expect(lag).toBeLessThan(0.05);
      // A turn may end a frame before the next is queued: never more than a frame's stop.
      const stops = stopsFrom(run, 20_000);
      expect(Math.max(0, ...stops)).toBeLessThanOrEqual(1);
      // At most 3 per 100 turns (at least one allowed).
      expect(stops.length).toBeLessThanOrEqual(
        Math.max(1, (40_000 / pace) * 0.03),
      );
    }
  });

  test("at a 0.5 s pace a watcher falls further behind, and a hand player doesn't", () => {
    const watching = lagsFrom(
      live(() => 500, 60_000),
      20_000,
    );
    const playing = lagsFrom(
      live(() => 500, 60_000, { timing: PLAYING }),
      20_000,
    );
    expect(Math.min(...watching)).toBeGreaterThan(2);
    expect(Math.max(...playing)).toBeLessThan(0.05);
  });

  test("a late turn stops the board, and the lag doesn't stay", () => {
    // Turn 20 comes 300 ms late; the others keep the server's pace.
    const run = live(
      (index) => (index === 20 ? 1300 : index === 21 ? 700 : 1000),
      40_000,
      { timing: PLAYING },
    );
    // The board waited about 300 ms for it.
    expect(Math.max(...stopsFrom(run, 20_000))).toBeGreaterThan(250 / FRAME_MS);
    // Three turns later it is back with the open generation.
    for (const lag of lagsFrom(run, 24_600)) expect(lag).toBeLessThan(0.1);
  });

  test("two turns that arrive together are caught up within a few turns, without a jump", () => {
    const run = live(
      (index) => (index === 20 ? 2000 : index === 21 ? 0 : 1000),
      40_000,
      { timing: PLAYING },
    );
    for (const lag of lagsFrom(run, 25_600)) expect(lag).toBeLessThan(0.1);
    expect(Math.max(...run.speeds)).toBeLessThanOrEqual(1.5);
    for (let index = 1; index < run.speeds.length; index++) {
      const change = Math.abs(
        (run.speeds[index] as number) - (run.speeds[index - 1] as number),
      );
      expect(change).toBeLessThan(0.05);
    }
  });

  test("with jitter on the arrivals a hand player's lag stays small", () => {
    seed = 7;
    const run = live(
      () => 1020 + 80 * ((uneven() - 600) / 800 - 0.5),
      120_000,
      {
        timing: PLAYING,
      },
    );
    const lags = lagsFrom(run, 20_000).sort((a, b) => a - b);
    expect(lags[Math.floor(lags.length * 0.95)] as number).toBeLessThan(0.1);
  });

  test("switching from PLAYING to WATCHING eases to a watcher's lag, without a step", () => {
    const run = live(() => 1000, 60_000, {
      timing: PLAYING,
      switchTo: { at: 30_000, timing: WATCHING },
    });
    for (let index = 1; index < run.speeds.length; index++) {
      const change = Math.abs(
        (run.speeds[index] as number) - (run.speeds[index - 1] as number),
      );
      expect(change).toBeLessThan(0.05);
    }
    for (const lag of lagsFrom(run, 50_000)) {
      expect(lag).toBeGreaterThan(0.9);
      expect(lag).toBeLessThan(1.1);
    }
  });

  test("after a hidden tab, switching to WATCHING keeps no more than a turn behind", () => {
    // Hidden for 6 s while playing by hand; the team is out 20 s later.
    const run = live(() => 1000, 80_000, {
      timing: PLAYING,
      hidden: [20_000, 26_000],
      switchTo: { at: 40_000, timing: WATCHING },
    });
    expect(run.player.liveTiming).toBe(WATCHING);
    for (const lag of lagsFrom(run, 60_000)) expect(lag).toBeLessThan(1.1);
  });

  test("switching from WATCHING to PLAYING catches up without a step", () => {
    const run = live(() => 1000, 60_000, {
      switchTo: { at: 30_000, timing: PLAYING },
    });
    for (let index = 1; index < run.speeds.length; index++) {
      const change = Math.abs(
        (run.speeds[index] as number) - (run.speeds[index - 1] as number),
      );
      expect(change).toBeLessThan(0.05);
    }
    for (const lag of lagsFrom(run, 40_000)) expect(lag).toBeLessThan(0.05);
  });
});

describe("a hand player's timing with the game's pace (#345)", () => {
  for (const pace of [500, 1000]) {
    for (const late of [2000, 4000]) {
      test(`at a ${pace} ms pace, a turn ${late} ms late leaves no lag two turns on`, () => {
        // Turn 20 comes late; the server keeps its pace from there.
        const run = live(
          (index) => (index === 20 ? pace + late : pace),
          (30 + late / pace) * pace + 30 * pace,
          { timing: PLAYING, paceMs: pace },
        );
        // Before it, and from two turns after it, the board is with the open generation.
        for (const lag of lagsFromTurn(run, 5).slice(0, 14)) {
          expect(lag).toBeLessThan(0.1);
        }
        const after = run.lags.filter((entry) => entry.index >= 23);
        expect(after.length).toBeGreaterThan(10);
        for (const { lag } of after) expect(lag).toBeLessThan(0.1);
        // The turn length stays the pace.
        expect(run.player.turnDuration).toBe(pace);
      });
    }

    test(`at a ${pace} ms pace, the first turns and the turns after a jump are on time`, () => {
      const run = live(() => pace, 60 * pace, {
        timing: PLAYING,
        paceMs: pace,
        jumpAt: 30 * pace + 250,
      });
      // From the second turn of the game on, including the turns right after the jump.
      for (const lag of lagsFromTurn(run, 2)) expect(lag).toBeLessThan(0.1);
    });
  }

  test("without the pace, a late turn stretches the turns after it (why the pace is passed)", () => {
    const run = live((index) => (index === 20 ? 3000 : 1000), 50_000, {
      timing: PLAYING,
    });
    expect(Math.max(...lagsFromTurn(run, 23))).toBeGreaterThan(0.3);
  });

  test("the pace changes nothing for a watcher", () => {
    seed = 1;
    const { trace } = live(() => 1000, 30_000, { paceMs: 1000 });
    for (const [index, position] of trace.entries()) {
      expect(position).toBeCloseTo(watchingTrace.steady[index] as number, 9);
    }
  });
});
