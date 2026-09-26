import { protocol } from "./errors.js";
import type { components } from "./generated/api.js";

type Wire = components["schemas"];
export type TeamType = "friendly" | "neutral" | "enemy";
export const NEUTRAL_TEAM_COLOR = 0;

export class BoardEntity {
  constructor(
    public readonly teamColor: number,
    public readonly x: number,
    public readonly y: number,
  ) {}
  distance(other: BoardEntity): number {
    return Math.hypot(other.x - this.x, other.y - this.y);
  }
  compareTeam(other: BoardEntity): TeamType {
    return other.teamColor === NEUTRAL_TEAM_COLOR
      ? "neutral"
      : this.teamColor === other.teamColor
        ? "friendly"
        : "enemy";
  }
}

export class Castle extends BoardEntity {
  readonly id: number;
  readonly soldiers: number;
  constructor(wire: Wire["CastleView"]) {
    super(wire.team_color, wire.x, wire.y);
    this.id = wire.id;
    this.soldiers = wire.soldiers;
    Object.freeze(this);
  }
}

export class SoldierGroup extends BoardEntity {
  readonly id: number;
  readonly soldiers: number;
  readonly originCastleId: number;
  readonly destinationCastleId: number;
  readonly clashRadius: number;
  constructor(
    wire: Wire["SoldierGroupView"],
    public readonly destinationCastle: Castle,
  ) {
    super(wire.team_color, wire.x, wire.y);
    this.id = wire.id;
    this.soldiers = wire.soldiers;
    this.originCastleId = wire.origin_persistent_castle_id;
    this.destinationCastleId = wire.destination_persistent_castle_id;
    this.clashRadius = wire.clash_radius;
    Object.freeze(this);
  }
  distanceLeft(): number {
    return this.distance(this.destinationCastle);
  }
}

export interface ClashParticipant {
  readonly groupId: number;
  readonly teamColor: number;
  readonly x: number;
  readonly y: number;
  readonly clashRadius: number;
  readonly soldiersBefore: number;
  readonly soldiersAfter: number;
}
export interface Clash {
  readonly id: number;
  readonly time: number;
  readonly participants: readonly ClashParticipant[];
}
export interface Generation {
  readonly id: number;
  readonly castles: readonly Castle[];
  readonly soldierGroups: readonly SoldierGroup[];
  readonly clashes: readonly Clash[];
}

/** Called only after validating the wire state against the generated schema. */
export function generationFromState(
  state: Wire["GenerationState"],
): Generation {
  const castles = state.generation.castles.map((castle) => new Castle(castle));
  const byId = new Map(castles.map((castle) => [castle.id, castle]));
  const soldierGroups = state.generation.soldier_groups.map((group) => {
    const destination = byId.get(group.destination_persistent_castle_id);
    if (!destination)
      throw protocol("soldier group destination castle is missing");
    return new SoldierGroup(group, destination);
  });
  const clashes = (state.generation.clashes ?? []).map((clash) =>
    Object.freeze({
      id: clash.id,
      time: clash.time,
      participants: Object.freeze(
        clash.participants.map((participant) =>
          Object.freeze({
            groupId: participant.group_id,
            teamColor: participant.team_color,
            x: participant.x,
            y: participant.y,
            clashRadius: participant.clash_radius,
            soldiersBefore: participant.soldiers_before,
            soldiersAfter: participant.soldiers_after,
          }),
        ),
      ),
    }),
  );
  return Object.freeze({
    id: state.generation_id,
    castles: Object.freeze(castles),
    soldierGroups: Object.freeze(soldierGroups),
    clashes: Object.freeze(clashes),
  });
}
