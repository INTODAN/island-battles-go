// The Controls panel of the game view (#345): what a hand player can do on the board, written
// from one table, and whether the panel opens by itself. No DOM: `screen.ts` draws it.

/** One action, and how to do it with a mouse and keyboard, and on a touch screen. */
export interface ControlRow {
  readonly action: string;
  /** Null: there is no such control with a mouse and keyboard. */
  readonly mouse: string | null;
  /** Null: there is no such control on a touch screen. */
  readonly touch: string | null;
}

/**
 * The controls of hand play: drag to send and the amount (#343), the castle menu (#344), the fit
 * button (#346) and the camera. Keys are written in brackets, `[d]`, so the panel draws them as keys.
 */
export const controlTable: readonly ControlRow[] = [
  {
    action: "Send soldiers",
    mouse:
      "Drag from your castle to another castle. Every drag sends all the castle's soldiers unless you change the amount.",
    touch:
      "Drag with one finger from your castle to another castle. Every drag sends all the castle's soldiers unless you change the amount.",
  },
  {
    action: "All",
    mouse: "[d] while dragging",
    touch: "Tap All, right of the amount slider",
  },
  {
    action: "Half",
    mouse: "[a] while dragging",
    touch: "Tap Half, left of the amount slider",
  },
  {
    action: "More, fewer",
    mouse:
      "[w] and [s] while dragging, one soldier more or fewer; hold to repeat",
    touch: null,
  },
  {
    action: "An exact number",
    mouse: "Type the digits while dragging; [Backspace] edits them",
    touch: null,
  },
  {
    action: "The amount slider",
    mouse:
      "Above the timeline: before a drag it sets the share for the next drag, during a drag the soldiers of that drag. The amount goes back to all after every drag.",
    touch:
      "Above the timeline: before a drag it sets the share for the next drag, during a drag the soldiers of that drag. The amount goes back to all after every drag.",
  },
  {
    action: "Castle menu: upgrades and burst",
    mouse: "Right-click or double-click your castle",
    touch: "Double-tap your castle",
  },
  {
    action: "Cancel",
    mouse: "[Esc], the right button, or release away from a castle",
    touch:
      "Lift your finger away from a castle, or touch the board with a second finger",
  },
  {
    action: "Fit the board",
    mouse: "[F], or the fit button at the top right",
    touch: "The fit button at the top right",
  },
  {
    action: "Turn, pan and zoom the board",
    mouse:
      "Drag anywhere except from one of your castles; right-drag to pan; the wheel to zoom",
    touch:
      "Drag anywhere except from one of your castles; two fingers to pan; pinch to zoom",
  },
];

/** The rows for a touch screen, or for a mouse and keyboard. */
export function controlRows(touch: boolean): { action: string; how: string }[] {
  const rows: { action: string; how: string }[] = [];
  for (const row of controlTable) {
    const how = touch ? row.touch : row.mouse;
    if (how !== null) rows.push({ action: row.action, how });
  }
  return rows;
}

/**
 * Which rows to show first, and whether the device has both kinds of pointer, so the panel offers
 * to switch. `matches` is `matchMedia(query).matches`. The primary pointer decides, as it decides
 * the size of the board's buttons (`pointer: coarse` in `game.css`).
 */
export function pointerRows(matches: (query: string) => boolean): {
  touch: boolean;
  canSwitch: boolean;
} {
  const touch = matches("(pointer: coarse)");
  const other = touch
    ? matches("(any-pointer: fine)")
    : matches("(any-pointer: coarse)");
  return { touch, canSwitch: other };
}

/** Set once the panel has opened by itself, so it does so only in a player's first hand game. */
export const controlsSeenKey = "island-battles-web.controls-seen";

/**
 * Whether the panel should open by itself. False where storage can't be read: better not to open
 * it in every game than to open it each time.
 */
export function shouldOpenControls(storage: () => Storage | null): boolean {
  try {
    const store = storage();
    if (!store) return false;
    return store.getItem(controlsSeenKey) === null;
  } catch {
    return false;
  }
}

/** Remembers that the panel opened by itself. */
export function markControlsSeen(storage: () => Storage | null): void {
  try {
    storage()?.setItem(controlsSeenKey, "1");
  } catch {
    // Storage is blocked or full: the panel may open by itself again.
  }
}

/** A key as the table writes it: in brackets. */
const keyPattern = /\[([^\]]+)\]/g;

/** A row's text in parts, the keys apart, so the panel can draw them as keys. */
export function controlParts(how: string): { text: string; key: boolean }[] {
  const parts: { text: string; key: boolean }[] = [];
  let at = 0;
  for (const match of how.matchAll(keyPattern)) {
    const index = match.index ?? 0;
    if (index > at) parts.push({ text: how.slice(at, index), key: false });
    parts.push({ text: match[1] ?? "", key: true });
    at = index + match[0].length;
  }
  if (at < how.length) parts.push({ text: how.slice(at), key: false });
  return parts;
}
