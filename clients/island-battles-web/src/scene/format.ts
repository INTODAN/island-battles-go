import type { components } from "../api/generated/game";

type Castle = components["schemas"]["Castle"];
type SoldierGroup = components["schemas"]["SoldierGroup"];
type GameTeam = components["schemas"]["GameTeam"];

// Known upgrade types come first under short names; any other type follows under its own name.
// This is the replay's own wording (backend/services/replay.html), such as "Speed 2 · Stop 1".
const upgradeNames = new Map([
  ["speed", "Speed"],
  ["clash_stop_generations", "Stop"],
  ["generation_amount", "Growth"],
  ["burst_potential", "Burst"],
]);
const upgradeRank = (type: string) =>
  [...upgradeNames.keys()].indexOf(type) >>> 0;

export function upgradeLevels(upgrades: Record<string, number>): string {
  return Object.keys(upgrades)
    .sort(
      (left, right) =>
        upgradeRank(left) - upgradeRank(right) || left.localeCompare(right),
    )
    .map((type) => `${upgradeNames.get(type) ?? type} ${upgrades[type]}`)
    .join(" · ");
}

/** Whether any upgrade is above the starting level 1, so a label shows the upgrade line. */
export function hasUpgrades(upgrades: Record<string, number>): boolean {
  return Object.values(upgrades).some((level) => level > 1);
}

export function teamName(
  teams: readonly GameTeam[],
  teamColor: number,
): string {
  if (teamColor === 0) return "Neutral";
  return (
    teams.find((team) => team.color === teamColor)?.name ?? `Team ${teamColor}`
  );
}

export function castleTitle(castle: Castle): string {
  return `Castle ${castle.id + 1}`;
}

export function castleTooltip(
  castle: Castle,
  teams: readonly GameTeam[],
): string[] {
  return [
    `${castleTitle(castle)} (ID ${castle.id})`,
    `${teamName(teams, castle.team_color)} · ${castle.soldiers} soldiers`,
    `Upgrades: ${upgradeLevels(castle.upgrades) || "none"}`,
    ...burstLines(castle),
  ];
}

/** A castle's burst, when it has one: pending until the next turn pays it out, then owed. */
export function burstLines(castle: Castle): string[] {
  const lines: string[] = [];
  if (castle.pending_burst > 0) {
    lines.push(`Burst of ${castle.pending_burst} arrives next turn`);
  }
  if (castle.burst_debt > 0) {
    lines.push(`Burst debt: ${castle.burst_debt} soldiers`);
  }
  return lines;
}

export function groupTooltip(
  group: SoldierGroup,
  teams: readonly GameTeam[],
): string[] {
  return [
    `Group ${group.id}`,
    `${teamName(teams, group.team_color)} · ${group.soldiers} soldiers`,
    `Heading for Castle ${group.destination_castle_id + 1} (from Castle ${group.origin_castle_id + 1})`,
    `Upgrades: ${upgradeLevels(group.upgrades) || "none"}`,
  ];
}
