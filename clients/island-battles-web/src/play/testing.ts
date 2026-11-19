// Fixtures for hand play's tests: a board, the upgrade table, game states and a fake clock.

import type { GenerationState } from "../realtime/install";
import type { Generation, UpgradeTable } from "./rules";

/** The server's upgrade table (backend/models/upgrades.go), as `GET /game/upgrades` serves it. */
export const upgradeTable: UpgradeTable = [
  {
    type: "burst_potential",
    description: "The largest burst",
    max_level: 4,
    levels: [
      { level: 1, value: 0, soldier_cost: 0 },
      { level: 2, value: 4, soldier_cost: 10 },
      { level: 3, value: 8, soldier_cost: 20 },
      { level: 4, value: 16, soldier_cost: 30 },
    ],
  },
  {
    type: "clash_stop_generations",
    description: "Generations a clash stops a group",
    max_level: 3,
    levels: [
      { level: 1, value: 3, soldier_cost: 0 },
      { level: 2, value: 2, soldier_cost: 10 },
      { level: 3, value: 1, soldier_cost: 20 },
    ],
  },
  {
    type: "generation_amount",
    description: "Soldiers produced each generation",
    max_level: 4,
    levels: [
      { level: 1, value: 1, soldier_cost: 0 },
      { level: 2, value: 2, soldier_cost: 10 },
      { level: 3, value: 3, soldier_cost: 20 },
      { level: 4, value: 4, soldier_cost: 30 },
    ],
  },
  {
    type: "speed",
    description: "World units a turn",
    max_level: 4,
    levels: [
      { level: 1, value: 0.5, soldier_cost: 0 },
      { level: 2, value: 1, soldier_cost: 10 },
      { level: 3, value: 1.5, soldier_cost: 20 },
      { level: 4, value: 2, soldier_cost: 30 },
    ],
  },
];

const levels = (overrides: Record<string, number> = {}) => ({
  burst_potential: 1,
  clash_stop_generations: 1,
  generation_amount: 1,
  speed: 1,
  ...overrides,
});

/**
 * A board for team 1: castle 1 (30 soldiers) and castle 2 (12, Burst 2) are team 1's, castle 3
 * is team 2's, castle 4 is neutral.
 */
export function board(id = 10, extra: Partial<Generation> = {}): Generation {
  return {
    id,
    castles: [
      {
        id: 1,
        team_color: 1,
        soldiers: 30,
        burst_debt: 0,
        upgrades: levels(),
        x: 0,
        y: 0,
      },
      {
        id: 2,
        team_color: 1,
        soldiers: 12,
        burst_debt: 0,
        upgrades: levels({ burst_potential: 2 }),
        x: 1,
        y: 0,
      },
      {
        id: 3,
        team_color: 2,
        soldiers: 20,
        burst_debt: 0,
        upgrades: levels(),
        x: 2,
        y: 0,
      },
      {
        id: 4,
        team_color: 0,
        soldiers: 10,
        burst_debt: 0,
        upgrades: levels(),
        x: 3,
        y: 0,
      },
    ],
    soldier_groups: [],
    clashes: [],
    ...extra,
  };
}

/** A hand player's state of generation `id` in a real-time game at a 1 s pace. */
export function handState(
  id: number,
  extra: Partial<GenerationState> = {},
  createdAt = 1_000_000,
): GenerationState {
  return {
    bot: { kind: "manual" },
    channel: "player:p1",
    game_id: 7,
    generation: board(id),
    generation_id: id,
    lobby_id: "lobby",
    lobby_round: 1,
    log_lines_stored: 0,
    memory: null,
    player_id: "p1",
    relayed_bots: [],
    submitted: false,
    team_color: 1,
    turn_number: id,
    max_generations: 300,
    turn_deadline_at: new Date(createdAt + 1000).toISOString(),
    turn_pace_milliseconds: 1000,
    ...extra,
  } as GenerationState;
}

export const flush = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve();
};

/** A fake clock whose timers run only when the test advances it. */
export function fakeClock(start = 1_000_000) {
  let now = start;
  let nextId = 1;
  const timers = new Map<number, { at: number; run: () => void }>();
  return {
    now: () => now,
    schedule(run: () => void, ms: number) {
      const id = nextId++;
      timers.set(id, { at: now + Math.max(0, ms), run });
      return () => {
        timers.delete(id);
      };
    },
    async advance(ms: number) {
      const target = now + ms;
      await flush();
      for (;;) {
        const due = [...timers.entries()]
          .filter(([, timer]) => timer.at <= target)
          .sort((a, b) => a[1].at - b[1].at || a[0] - b[0])[0];
        if (!due) break;
        timers.delete(due[0]);
        now = Math.max(now, due[1].at);
        due[1].run();
        await flush();
      }
      now = target;
      await flush();
    },
    pending: () => timers.size,
  };
}
