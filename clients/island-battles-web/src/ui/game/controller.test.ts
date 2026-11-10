import { describe, expect, test } from "bun:test";
import { GameController, type PlayerPort } from "./controller";
import type { Generation } from "./history";
import { generation } from "./testing";

const outcome = {
  reason: "last_team_standing",
  winner_team_color: 1,
  summary: "Blue wins as the last team standing",
} as const;

/** A player that records calls and plays a turn until `finish` is called. */
class FakePlayer implements PlayerPort {
  calls: string[] = [];
  pending = 0;
  following = false;
  get busy() {
    return this.pending > 0;
  }
  play(previous: Generation, next: Generation) {
    this.calls.push(`play ${previous.id}>${next.id}`);
    this.pending++;
  }
  seek(generation: Generation) {
    this.calls.push(`seek ${generation.id}`);
    this.pending = 0;
  }
  finish() {
    this.pending = Math.max(0, this.pending - 1);
  }
  setFollowing(following: boolean) {
    this.following = following;
  }
}

const frames = (...ids: number[]) => ids.map((id) => generation(id));

function live(...ids: number[]) {
  const player = new FakePlayer();
  const controller = new GameController(player, { live: true });
  controller.load(frames(...ids), null);
  player.calls = [];
  return { player, controller };
}

describe("opening the view", () => {
  test("mid-game it shows the newest turn and follows live", () => {
    const player = new FakePlayer();
    const controller = new GameController(player, { live: true });
    controller.load(frames(10, 11, 14), null);
    expect(controller.state).toMatchObject({ index: 2, follow: true });
    expect(player.calls).toEqual(["seek 14"]);
    expect(controller.state.frames).toHaveLength(3);
  });

  test("a finished game opens on its newest turn without following", () => {
    const player = new FakePlayer();
    const controller = new GameController(player, { live: true });
    controller.load(frames(1, 2, 3), outcome);
    expect(controller.state).toMatchObject({ index: 2, follow: false });
  });

  test("a demo file opens on its first turn, paused", () => {
    const player = new FakePlayer();
    const controller = new GameController(player, { live: false });
    controller.load(frames(1, 2, 3), outcome);
    expect(controller.state).toMatchObject({
      index: 0,
      follow: false,
      playing: false,
    });
    expect(player.calls).toEqual(["seek 1"]);
  });
});

describe("live turns", () => {
  test("a new turn plays while following and moves the turn number", () => {
    const { player, controller } = live(1, 2);
    controller.merge(frames(3));
    expect(player.calls).toEqual(["play 2>3"]);
    expect(controller.state).toMatchObject({ index: 2, follow: true });
  });

  test("a repeated turn changes nothing", () => {
    const { player, controller } = live(1, 2);
    controller.merge(frames(2));
    expect(player.calls).toEqual([]);
    expect(controller.state.frames).toHaveLength(2);
  });

  test("turns that arrive together queue in order", () => {
    const { player, controller } = live(1, 2);
    controller.merge(frames(5, 3, 4));
    expect(player.calls).toEqual(["play 2>3", "play 3>4", "play 4>5"]);
  });

  test("turns missed during a disconnect fill in behind the newest, numbered by index", () => {
    const { player, controller } = live(1, 2);
    controller.merge(frames(5));
    controller.merge(frames(3, 4, 5));
    // The view jumped from 2 to 5; 3 and 4 are history now, with 5 at index 4.
    expect(player.calls).toEqual(["play 2>5"]);
    expect(controller.state.frames.map((frame) => frame.id)).toEqual([
      1, 2, 3, 4, 5,
    ]);
    expect(controller.state).toMatchObject({ index: 4, follow: true });
  });

  test("a turn that arrives while scrubbed back is added without moving the view", () => {
    const { player, controller } = live(1, 2, 3);
    controller.scrubTo(0);
    player.calls = [];
    controller.merge(frames(4));
    expect(player.calls).toEqual([]);
    expect(controller.state).toMatchObject({ index: 0, follow: false });
    expect(controller.state.frames).toHaveLength(4);
  });

  test("the final turn arriving with the outcome still plays", () => {
    const { player, controller } = live(1, 2);
    controller.merge(frames(3), outcome);
    expect(player.calls).toEqual(["play 2>3"]);
    expect(controller.state).toMatchObject({ index: 2, follow: false });
  });

  test("the outcome ends following and keeps the timeline usable", () => {
    const { controller } = live(1, 2);
    controller.merge(frames(3), outcome);
    expect(controller.state.outcome).toEqual(outcome);
    expect(controller.state.follow).toBe(false);
    controller.scrubTo(0);
    expect(controller.state.index).toBe(0);
    controller.scrubTo(2);
    expect(controller.state).toMatchObject({ index: 2, follow: false });
  });
});

describe("follow live", () => {
  test("turns off when scrubbing back and on at the newest turn", () => {
    const { controller } = live(1, 2, 3, 4);
    expect(controller.state.follow).toBe(true);
    controller.scrubTo(1);
    expect(controller.state.follow).toBe(false);
    controller.scrubTo(2);
    expect(controller.state.follow).toBe(false);
    controller.scrubTo(3);
    expect(controller.state.follow).toBe(true);
  });

  test("stepping back turns it off, stepping forward onto the newest turn turns it on", () => {
    const { player, controller } = live(1, 2, 3);
    controller.step(-1);
    expect(controller.state).toMatchObject({ index: 1, follow: false });
    controller.step(1);
    expect(player.calls).toContain("play 2>3");
    expect(controller.state).toMatchObject({ index: 2, follow: true });
  });

  test("switching it on jumps forward through the turns missed", () => {
    const { player, controller } = live(1, 2, 3, 4);
    controller.scrubTo(1);
    player.calls = [];
    controller.setFollow(true);
    expect(player.calls).toEqual(["play 2>3", "play 3>4"]);
    expect(controller.state).toMatchObject({ index: 3, follow: true });
  });

  test("a demo cannot follow", () => {
    const player = new FakePlayer();
    const controller = new GameController(player, { live: false });
    controller.load(frames(1, 2), null);
    controller.setFollow(true);
    expect(controller.state.follow).toBe(false);
    controller.scrubTo(1);
    expect(controller.state.follow).toBe(false);
  });

  test("pausing while following stops following", () => {
    const { controller } = live(1, 2);
    controller.togglePlay();
    expect(controller.state).toMatchObject({ follow: false, playing: false });
  });

  test("the player adapts its pace only while following live (#176)", () => {
    const { player, controller } = live(1, 2, 3);
    expect(player.following).toBe(true);
    controller.scrubTo(0);
    expect(player.following).toBe(false);
    controller.togglePlay();
    expect(player.following).toBe(false);
    controller.setFollow(true);
    expect(player.following).toBe(true);

    const demo = new FakePlayer();
    new GameController(demo, { live: false }).load(frames(1, 2), null);
    expect(demo.following).toBe(false);
  });
});

describe("play, step and scrub", () => {
  test("scrubbing seeks and does not animate", () => {
    const { player, controller } = live(1, 2, 3);
    controller.scrubTo(0);
    expect(player.calls).toEqual(["seek 1"]);
    controller.scrubTo(99);
    expect(controller.state.index).toBe(2);
    controller.scrubTo(-5);
    expect(controller.state.index).toBe(0);
  });

  test("play queues one turn at a time as each finishes", () => {
    const player = new FakePlayer();
    const controller = new GameController(player, { live: false });
    controller.load(frames(1, 2, 3), outcome);
    player.calls = [];
    controller.togglePlay();
    expect(controller.state.playing).toBe(true);
    controller.tick();
    expect(player.calls).toEqual(["play 1>2"]);
    controller.tick();
    expect(player.calls).toEqual(["play 1>2"]);
    player.finish();
    controller.tick();
    expect(player.calls).toEqual(["play 1>2", "play 2>3"]);
    expect(controller.state).toMatchObject({ index: 2, playing: false });
  });

  test("play at the end of a finished game starts again from the first turn", () => {
    const player = new FakePlayer();
    const controller = new GameController(player, { live: false });
    controller.load(frames(1, 2), outcome);
    controller.scrubTo(1);
    player.calls = [];
    controller.togglePlay();
    expect(player.calls).toEqual(["seek 1"]);
    expect(controller.state).toMatchObject({ index: 0, playing: true });
  });

  test("playing a live game through to the newest turn follows live again", () => {
    const { player, controller } = live(1, 2, 3);
    controller.scrubTo(0);
    controller.togglePlay();
    controller.tick();
    player.finish();
    controller.tick();
    expect(controller.state).toMatchObject({
      index: 2,
      playing: false,
      follow: true,
    });
  });

  test("scrubbing stops playing", () => {
    const player = new FakePlayer();
    const controller = new GameController(player, { live: false });
    controller.load(frames(1, 2, 3), outcome);
    controller.togglePlay();
    controller.scrubTo(2);
    expect(controller.state.playing).toBe(false);
  });

  test("subscribers hear every change", () => {
    const { controller } = live(1, 2);
    const seen: number[] = [];
    const stop = controller.subscribe((state) => seen.push(state.index));
    controller.merge(frames(3));
    controller.scrubTo(0);
    stop();
    controller.scrubTo(1);
    expect(seen).toEqual([2, 0]);
  });
});
