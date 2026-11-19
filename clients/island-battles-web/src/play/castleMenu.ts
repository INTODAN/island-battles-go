// The castle menu's rules (#344), with no DOM and no Babylon: when the menu opens and closes, what
// each row shows and why a row is disabled, and where the menu sits beside its castle. The board's
// controls (`src/scene/castleMenuInput.ts`) and the menu (`src/ui/game/castleMenu.ts`) only feed it
// and draw what it answers. Every number and refusal comes from hand play (`check`, the ledger and
// the upgrade table): nothing here re-derives a server rule.

import { castleTitle, upgradeName } from "../scene/format";
import type { CheckResult, PlayState, PlayStatus } from "./handPlay";
import {
  type CastleLedger,
  type OrderSpec,
  type Refusal,
  refusalSentence,
  type UpgradeType,
  upgradeCost,
  upgradeOf,
  upgradeValue,
} from "./rules";

/** How far a right-click may move, in CSS pixels, and still open the menu; a longer one pans. */
export const RIGHT_CLICK_SLOP_PX = 8;

/** The gestures that open the menu. */
export type MenuOpener = "right-click" | "double-click" | "double-tap";

/** What decides whether the menu may be open on a castle. */
export interface MenuGate {
  status: PlayStatus;
  /** Whether the view shows the newest turn of the running game (`showsNewestTurn`). */
  showsNewestTurn: boolean;
  /** Your castles in the open generation: `PlayState.castles`. */
  castles: ReadonlyMap<number, unknown>;
}

/**
 * Whether the menu may be open on `castleId`: for a hand player who is `playing`, while the view
 * shows the newest turn, on a castle your team owns. The same rule opens it and keeps it open, so
 * the menu closes when the castle is captured, the game ends, your team is out or the view leaves
 * the newest turn.
 */
export function menuAllowed(gate: MenuGate, castleId: number): boolean {
  return (
    gate.status === "playing" &&
    gate.showsNewestTurn &&
    gate.castles.has(castleId)
  );
}

/** Whether a right-click that went down at `down` and up at `up` is a click (it opens the menu), not a pan. */
export function isRightClick(
  down: { x: number; y: number },
  up: { x: number; y: number },
): boolean {
  return Math.hypot(up.x - down.x, up.y - down.y) < RIGHT_CLICK_SLOP_PX;
}

/**
 * How long a finger's second tap may come after the first and still be a double-tap. Babylon's own
 * double tap (`InputManager.DoubleClickDelay`) allows 300 ms, short for a finger; a second touch tap
 * on the same castle up to this long after the first opens the menu too.
 */
export const TOUCH_DOUBLE_TAP_MS = 450;

/** One press or tap as the board's controls see it: the button, the pointer, and the castle under it. */
export interface MenuPointer {
  /** `PointerEvent.button`: 0 primary, 2 right. */
  button: number;
  pointerType: string;
  /** The castle under the pointer, or null. */
  castleId: number | null;
  /** Client coordinates, in CSS pixels. */
  x: number;
  y: number;
  /** Milliseconds, from any clock that only goes forward. */
  at: number;
}

/**
 * Which of Babylon's pointer events open the menu. The board's controls feed it every right-button
 * press (`down`), every tap (`tap`, Babylon's POINTERTAP) and every double tap (`doubleTap`,
 * POINTERDOUBLETAP), and open the menu with the opener it answers.
 *
 * - A right-click opens it when the pointer moved less than 8 px since the press (Babylon's tap
 *   allows 10).
 * - A double-click or double-tap opens it only when both presses were on the same castle: Babylon
 *   counts two quick presses anywhere as a double tap.
 * - A finger's second tap on the same castle up to `TOUCH_DOUBLE_TAP_MS` after the first is a
 *   double-tap too.
 */
export class MenuGestures {
  /** Where the right button went down, while it is down. */
  private rightDown: { x: number; y: number } | null = null;
  /** The last primary tap that could start a double-click. */
  private lastTap: MenuPointer | null = null;

  down(pointer: MenuPointer): void {
    if (pointer.button === 2) this.rightDown = { x: pointer.x, y: pointer.y };
  }

  tap(pointer: MenuPointer): MenuOpener | null {
    if (pointer.button === 2) {
      const down = this.rightDown;
      this.rightDown = null;
      this.lastTap = null;
      return down && pointer.castleId !== null && isRightClick(down, pointer)
        ? "right-click"
        : null;
    }
    if (pointer.button !== 0) return null;
    const last = this.lastTap;
    if (
      pointer.pointerType === "touch" &&
      last?.pointerType === "touch" &&
      pointer.castleId !== null &&
      last.castleId === pointer.castleId &&
      pointer.at - last.at <= TOUCH_DOUBLE_TAP_MS
    ) {
      this.lastTap = null;
      return "double-tap";
    }
    this.lastTap = pointer;
    return null;
  }

  doubleTap(pointer: MenuPointer): MenuOpener | null {
    const last = this.lastTap;
    this.lastTap = null;
    if (
      pointer.button !== 0 ||
      pointer.castleId === null ||
      last?.castleId !== pointer.castleId
    ) {
      return null;
    }
    return pointer.pointerType === "touch" ? "double-tap" : "double-click";
  }
}

/**
 * Whether a double-click or double-tap on a castle focuses the camera on it. Not for a hand player
 * while they play: there it opens the menu on their own castle and does nothing on any other.
 * Watchers, demos, players with a bot, and a hand player whose team is out or whose game has ended
 * keep the focus. `status` is null without hand play.
 */
export function doubleClickFocuses(status: PlayStatus | null): boolean {
  return status !== "playing";
}

/** How the menu words each upgrade type: what it does, and the unit of its values. */
const upgradeWords: Record<
  UpgradeType,
  { description: string; unit: (value: string) => string }
> = {
  speed: {
    description: "How far the groups sent from this castle sail each turn.",
    unit: (value) => `${value} units a turn`,
  },
  clash_stop_generations: {
    description:
      "How many turns this castle's groups stay stopped after a clash. Lower is better.",
    unit: (value) => `${value} turns stopped`,
  },
  generation_amount: {
    description: "Soldiers this castle produces each turn.",
    unit: (value) => `${value} soldiers a turn`,
  },
  burst_potential: {
    description:
      "The most soldiers one burst can bring in. The castle repays them from its production.",
    unit: (value) => `${value} soldiers a burst`,
  },
};

/** The order of the rows: the board's labels' order (Speed, Stop, Growth, Burst). */
export const menuUpgradeOrder: readonly UpgradeType[] = [
  "speed",
  "clash_stop_generations",
  "generation_amount",
  "burst_potential",
];

const soldiers = (count: number) =>
  count === 1 ? "1 soldier" : `${count} soldiers`;

/** The menu's own short words for a refusal; any other refusal gets hand play's sentence. */
export function menuReason(refusal: Refusal): string {
  switch (refusal.kind) {
    case "upgrade-max-level":
      return "Maximum level";
    case "insufficient-soldiers":
      return `Needs ${soldiers(refusal.needed)}; the castle has ${refusal.unreserved}`;
    case "no-burst-potential":
      return "No burst potential yet";
    case "burst-debt":
      return `Repaying a burst: ${soldiers(refusal.debt)} left`;
    case "burst-ordered":
      return "Already ordered this turn";
    case "upgrades-not-loaded":
      return LOADING_REASON;
    default:
      return refusalSentence(refusal);
  }
}

/** Shown on every row until `GET /game/upgrades` has loaded: the costs and potentials are unknown. */
export const LOADING_REASON = "Loading the costs…";

export interface UpgradeRow {
  type: UpgradeType;
  /** "Speed". */
  name: string;
  description: string;
  /** The level counting this turn's upgrades: the next purchase buys `level + 1`. */
  level: number;
  /** The level the castle had when the turn opened. */
  levelAtTurnStart: number;
  /** Levels of this type ordered this turn. */
  ordered: number;
  /** The next level, or null at the maximum. */
  next: number | null;
  /** "Speed 2 → 3: 1 → 1.5 units a turn", or "Speed 4: 2 units a turn" at the maximum. */
  effect: string;
  /** "Speed 2 ordered this turn" (or "Speed 2 and 3 …"), or null when none is. */
  orderedText: string | null;
  /** The soldiers the next level costs, or null at the maximum or before the table loaded. */
  cost: number | null;
  /** Why the row can't be bought now, or null when it can. */
  disabled: string | null;
}

export interface BurstRow {
  /** The largest burst now, counting this turn's upgrades; null before the table loaded. */
  potential: number | null;
  /** The castle's burst debt. */
  debt: number;
  /** The slider's range is 1 to `max` (0 when it can't burst). */
  max: number;
  /** Why the castle can't burst now, or null when it can. */
  disabled: string | null;
  /** The soldiers of the burst ordered this turn, or null when none is. */
  ordered: number | null;
}

export interface CastleMenuModel {
  castleId: number;
  /** "Castle 3". */
  title: string;
  soldiers: number;
  /** What this turn's orders left: what an upgrade can still spend. */
  unreserved: number;
  /** "12 soldiers free to spend". */
  subtitle: string;
  upgrades: UpgradeRow[];
  burst: BurstRow;
}

/** What the model reads from hand play: its state and its check. */
export interface MenuPlay {
  state: Pick<PlayState, "castles" | "upgrades" | "orders">;
  check(spec: OrderSpec): CheckResult;
}

/** A level's value as the menu writes it: up to two decimals, no trailing zeros. */
function formatValue(value: number | undefined): string {
  return value === undefined ? "?" : String(Math.round(value * 100) / 100);
}

function upgradeRow(
  play: MenuPlay,
  castle: CastleLedger,
  type: UpgradeType,
): UpgradeRow {
  const table = play.state.upgrades;
  const level = castle.levels[type];
  const ordered = castle.ordered[type];
  const name = upgradeName(type);
  const words = upgradeWords[type];
  const maxLevel = upgradeOf(table, type)?.max_level;
  const atMax = maxLevel !== undefined && level >= maxLevel;
  const next = atMax ? null : level + 1;
  const now = formatValue(upgradeValue(table, type, level));
  const effect =
    next === null
      ? `${name} ${level}: ${words.unit(now)}`
      : `${name} ${level} → ${next}: ${words.unit(`${now} → ${formatValue(upgradeValue(table, type, next))}`)}`;
  const boughtLevels = Array.from(
    { length: ordered },
    (_, index) => level - ordered + 1 + index,
  );
  const orderedText =
    ordered === 0
      ? null
      : `${name} ${boughtLevels.join(" and ")} ordered this turn`;
  let disabled: string | null = null;
  if (table === null) {
    disabled = LOADING_REASON;
  } else {
    const result = play.check({
      kind: "upgrade",
      castleId: castle.id,
      upgradeType: type,
    });
    if (!result.ok) disabled = menuReason(result.refusal);
  }
  return {
    type,
    name,
    description: words.description,
    level,
    levelAtTurnStart: level - ordered,
    ordered,
    next,
    effect,
    orderedText,
    cost:
      next === null || table === null
        ? null
        : (upgradeCost(table, type, next) ?? null),
    disabled,
  };
}

function burstRow(play: MenuPlay, castle: CastleLedger): BurstRow {
  const potential = castle.burstPotential;
  const order = play.state.orders.find(
    (entry) =>
      entry.spec.kind === "burst" &&
      entry.spec.castleId === castle.id &&
      entry.state !== "rejected",
  );
  let disabled: string | null = null;
  if (play.state.upgrades === null || potential === null) {
    disabled = LOADING_REASON;
  } else if (castle.burstRefusal) {
    disabled = menuReason(castle.burstRefusal);
  } else {
    const result = play.check({
      kind: "burst",
      castleId: castle.id,
      amount: 1,
    });
    if (!result.ok) disabled = menuReason(result.refusal);
  }
  return {
    potential,
    debt: castle.burstDebt,
    max: Math.max(0, potential ?? 0),
    disabled,
    ordered: castle.bursting ? (order?.soldiers ?? null) : null,
  };
}

/** The menu of one of your castles, or null when the castle isn't yours in the open generation. */
export function castleMenuModel(
  play: MenuPlay,
  castleId: number,
): CastleMenuModel | null {
  const castle = play.state.castles.get(castleId);
  if (!castle) return null;
  return {
    castleId,
    title: castleTitle({ id: castleId }),
    soldiers: castle.soldiers,
    unreserved: castle.unreserved,
    subtitle: `${soldiers(castle.unreserved)} free to spend`,
    upgrades: menuUpgradeOrder.map((type) => upgradeRow(play, castle, type)),
    burst: burstRow(play, castle),
  };
}

/** The burst amount the slider shows: `wanted` kept within 1 and the potential (the potential when unset). */
export function burstAmount(wanted: number | null, max: number): number {
  if (max < 1) return 1;
  if (wanted === null || !Number.isFinite(wanted)) return max;
  return Math.min(max, Math.max(1, Math.round(wanted)));
}

/**
 * Where the menu goes: beside its castle at `anchor` (to its right, or its left when the right
 * has no room), vertically centred on it, and always inside the board (`board`, the canvas's size).
 * All in CSS pixels from the board's top left.
 */
export function placeMenu(
  anchor: { x: number; y: number },
  menu: { width: number; height: number },
  board: { width: number; height: number },
  gap = 28,
  margin = 6,
): { left: number; top: number } {
  const clamp = (value: number, low: number, high: number) =>
    Math.max(low, Math.min(value, Math.max(low, high)));
  const right = anchor.x + gap;
  const left =
    right + menu.width <= board.width - margin
      ? right
      : anchor.x - gap - menu.width >= margin
        ? anchor.x - gap - menu.width
        : anchor.x - menu.width / 2;
  return {
    left: clamp(left, margin, board.width - margin - menu.width),
    top: clamp(
      anchor.y - menu.height / 2,
      margin,
      board.height - margin - menu.height,
    ),
  };
}
