// The game's history as a list of generations, and what the HUD reads from it. Pure, so
// `bun test` covers it.

import type { components } from "../../api/generated/game";

export type Generation = components["schemas"]["Generation"];
export type GameTeam = components["schemas"]["GameTeam"];
export type GameOutcome = components["schemas"]["GameOutcome"];
export type Departure = components["schemas"]["Departure"];

/** What a team's card says about a team that left a real-time game. */
export function departureText(departure: Pick<Departure, "kind">): string {
  return departure.kind === "resigned" ? "Resigned" : "Left the game";
}

/**
 * The teams that left in turn `turn` (a turn is numbered by its index in the history, from 1), or
 * undefined when there are no departures to go by.
 */
export function teamsDepartedAt(
  departures: readonly Departure[],
  turn: number,
): ReadonlySet<number> | undefined {
  if (departures.length === 0) return undefined;
  return new Set(
    departures
      .filter((departure) => departure.turn_number === turn)
      .map((departure) => departure.team_color),
  );
}

/**
 * The departures from every source, one per team: the first list that names a team wins. The live
 * state lists the teams out so far; a game's outcome lists all of them (and an old demo's has
 * none).
 */
export function mergeDepartures(
  ...lists: ReadonlyArray<readonly Departure[] | undefined>
): Departure[] {
  const byTeam = new Map<number, Departure>();
  for (const list of lists) {
    for (const departure of list ?? []) {
      if (!byTeam.has(departure.team_color)) {
        byTeam.set(departure.team_color, departure);
      }
    }
  }
  return [...byTeam.values()];
}

/**
 * Merges `incoming` generations into `held` by `id`: one already held is ignored (the held one
 * stays, so a re-read of the same turn changes nothing), a new one is inserted in id order.
 * Generation IDs have gaps, so they say what comes before what and nothing else. Returns `held`
 * itself when nothing is new, so callers can tell cheaply.
 */
export function mergeGenerations(
  held: readonly Generation[],
  incoming: readonly Generation[],
): readonly Generation[] {
  const known = new Set(held.map((generation) => generation.id));
  const fresh: Generation[] = [];
  for (const generation of incoming) {
    if (known.has(generation.id)) continue;
    known.add(generation.id);
    fresh.push(generation);
  }
  if (fresh.length === 0) return held;
  return [...held, ...fresh].sort((left, right) => left.id - right.id);
}

/** A turn is numbered by its index in the history, from 1: "Turn 37 / 120". */
export function turnLabel(index: number, count: number): string {
  return `Turn ${index + 1} / ${count}`;
}

export interface TeamStats {
  team: GameTeam;
  /** Castles the team owns in the shown turn. */
  castles: number;
  /** Soldiers in its castles and on the way between them. */
  soldiers: number;
}

/** Each team's castle count and soldier total in a generation, in the order of `teams`. */
export function teamStats(
  generation: Generation,
  teams: readonly GameTeam[],
): TeamStats[] {
  return teams.map((team) => {
    const castles = generation.castles.filter(
      (castle) => castle.team_color === team.color,
    );
    const groups = generation.soldier_groups.filter(
      (group) => group.team_color === team.color,
    );
    const soldiers =
      castles.reduce((total, castle) => total + castle.soldiers, 0) +
      groups.reduce((total, group) => total + group.soldiers, 0);
    return { team, castles: castles.length, soldiers };
  });
}

/** Teams in colour order, as the lobby numbers them. */
export function sortTeams(teams: readonly GameTeam[]): GameTeam[] {
  return [...teams].sort((left, right) => left.color - right.color);
}
