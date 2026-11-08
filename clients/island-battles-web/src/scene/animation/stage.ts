import type { Scene } from "@babylonjs/core/scene";
import type { components } from "../../api/generated/game";
import { createBoat, disposeBoat } from "../art";
import {
  type Battlefield,
  boatScale,
  type GroupEntity,
  renderGeneration,
  updateCastle,
  updateGroup,
  updateHeadings,
} from "../battlefield";
import { castleDisplayLevel } from "../castleLevel";
import { toWorld } from "../coordinates";
import { createTurnEffects, type TurnEffects } from "./effects";
import type { TurnPlan, TurnSample } from "./plan";
import { type PlayerOptions, type Stage, TurnPlayer } from "./player";

type GameTeam = components["schemas"]["GameTeam"];
type Generation = components["schemas"]["Generation"];

/**
 * Applies a turn's samples to the meshes `renderGeneration` built, found by castle and group
 * ID. Groups that the next generation no longer has (arriving or defeated) get meshes of their
 * own for the length of the turn.
 */
export class SceneStage implements Stage {
  private readonly transients = new Map<number, GroupEntity>();
  private readonly hidden = new Set<number>();
  private readonly shownCounts = new Map<number, string>();
  private readonly shownCastles = new Map<number, string>();
  private readonly shownLevels = new Map<number, number>();
  private readonly shownTeams = new Map<number, number>();
  private field: Battlefield | undefined;

  constructor(
    private readonly scene: Scene,
    private readonly teams: readonly GameTeam[],
    readonly effects: TurnEffects = createTurnEffects(),
  ) {}

  settle(generation: Generation): void {
    this.reset();
    this.field = renderGeneration(this.scene, generation, this.teams);
    this.showClashes(this.field, true);
  }

  begin(plan: TurnPlan): void {
    this.reset();
    const field = renderGeneration(this.scene, plan.next, this.teams);
    this.field = field;
    // The next generation's clash rings, lost boats and loss labels appear as the turn plays.
    this.showClashes(field, false);
    for (const id of field.labels.ids()) {
      if (/^clash:/.test(id) || /^group:\d+:loss$/.test(id)) {
        field.labels.remove(id);
      }
    }
    const castles = new Map(
      [...plan.previous.castles, ...plan.next.castles].map((c) => [c.id, c]),
    );
    for (const group of plan.transients) {
      const parts = createBoat(
        this.scene,
        group.id,
        field.materials,
        group.team_color,
      );
      const entity = { parts };
      this.transients.set(group.id, entity);
      updateGroup(
        field,
        group,
        castles.get(group.destination_castle_id),
        entity,
      );
    }
    for (const castle of plan.previous.castles) {
      this.shownLevels.set(castle.id, castleDisplayLevel(castle.upgrades));
      this.shownTeams.set(castle.id, castle.team_color);
    }
  }

  apply(sample: TurnSample, plan: TurnPlan): void {
    const field = this.field;
    if (!field) return;
    const castles = new Map(
      [...plan.previous.castles, ...plan.next.castles].map((c) => [c.id, c]),
    );
    const groups = new Map(
      [...plan.transients, ...plan.next.soldier_groups].map((g) => [g.id, g]),
    );
    const moving: Generation["soldier_groups"] = [];
    for (const [id, pose] of sample.groups) {
      const group = groups.get(id);
      const entity = field.groups.get(id) ?? this.transients.get(id);
      if (!group || !entity) continue;
      const { root, body } = entity.parts;
      if (!pose.visible) {
        // A boat that is gone takes its labels with it; they would hang in the air.
        root.setEnabled(false);
        if (!this.hidden.has(id)) {
          this.hidden.add(id);
          for (const label of field.labels.ids()) {
            if (label.startsWith(`group:${id}:`)) field.labels.remove(label);
          }
        }
        continue;
      }
      if (this.hidden.delete(id)) this.shownCounts.delete(id);
      const key = `${pose.count}`;
      if (this.shownCounts.get(id) !== key) {
        this.shownCounts.set(id, key);
        updateGroup(
          field,
          { ...group, x: pose.x, y: pose.y, soldiers: pose.count },
          castles.get(group.destination_castle_id),
          entity,
        );
      }
      root.setEnabled(true);
      const world = toWorld(pose);
      root.position.set(world.x, 0, world.z);
      body.scaling.setAll(boatScale(pose.count) * pose.scale);
      if (pose.sink > 0) this.effects.sink(entity.parts, pose.sink);
      if (field.groups.has(id)) {
        moving.push({ ...group, x: pose.x, y: pose.y, soldiers: pose.count });
      }
    }

    for (const [id, pose] of sample.castles) {
      const castle = castles.get(id);
      const entity = field.castles.get(id);
      if (!castle || !entity) continue;
      // The level-up comes first: its pop swaps the model, so the update below keeps it.
      const level = this.shownLevels.get(id);
      if (level !== undefined && level !== pose.level) {
        this.effects.castleLevelUp(field, id, level, pose.level);
      }
      this.shownLevels.set(id, pose.level);
      const team = this.shownTeams.get(id);
      if (team !== undefined && team !== pose.team) {
        this.effects.castleCaptured(field, id, pose.team);
      }
      this.shownTeams.set(id, pose.team);
      const key = `${pose.count}:${pose.team}:${pose.level}`;
      if (this.shownCastles.get(id) !== key) {
        this.shownCastles.set(id, key);
        // The model of the level at this moment: the old one until the pop.
        updateCastle(
          field,
          { ...castle, soldiers: pose.count, team_color: pose.team },
          entity,
          pose.level,
        );
      }
    }

    this.effects.clash(field, sample.flashes);
    // The heading lines follow the boats as they sail.
    updateHeadings(field, {
      ...plan.next,
      castles: plan.next.castles,
      soldier_groups: moving,
    });
  }

  end(plan: TurnPlan): void {
    // A capture that lands as the turn ends has not been shown yet.
    const captured = plan.next.castles.filter((castle) => {
      const shown = this.shownTeams.get(castle.id);
      return shown !== undefined && shown !== castle.team_color;
    });
    this.reset();
    const field = renderGeneration(this.scene, plan.next, this.teams);
    this.field = field;
    this.showClashes(field, true);
    for (const castle of captured) {
      this.effects.castleCaptured(field, castle.id, castle.team_color);
    }
  }

  /** Drops what a turn in progress added, so the next draw starts from nothing half-way. */
  private reset(): void {
    this.effects.clear();
    for (const [id, entity] of this.transients) {
      for (const label of this.field?.labels.ids() ?? []) {
        if (label.startsWith(`group:${id}:`)) this.field?.labels.remove(label);
      }
      disposeBoat(entity.parts);
    }
    this.transients.clear();
    this.hidden.clear();
    this.shownCounts.clear();
    this.shownCastles.clear();
    this.shownLevels.clear();
    this.shownTeams.clear();
    const field = this.field;
    if (field) {
      for (const entity of field.groups.values()) {
        entity.parts.root.setEnabled(true);
      }
    }
  }

  private showClashes(field: Battlefield, visible: boolean): void {
    for (const entity of field.clashes.values()) {
      entity.ring.setEnabled(visible);
      entity.ghost?.root.setEnabled(visible);
    }
  }
}

export interface ScenePlayer {
  player: TurnPlayer;
  stage: SceneStage;
}

/**
 * A player for `scene`: it advances with the render loop. The game view drives it with
 * `play(previous, next)`, `seek(generation)` and `setSpeed`.
 */
export function createScenePlayer(
  scene: Scene,
  teams: readonly GameTeam[],
  options: PlayerOptions = {},
  effects: TurnEffects = createTurnEffects(scene),
): ScenePlayer {
  const stage = new SceneStage(scene, teams, effects);
  const player = new TurnPlayer(stage, options);
  scene.onBeforeRenderObservable.add(() => {
    player.tick(scene.getEngine().getDeltaTime());
    effects.tick(performance.now());
  });
  return { player, stage };
}
