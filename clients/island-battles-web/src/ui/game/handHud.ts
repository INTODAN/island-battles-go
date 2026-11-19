// The DOM of a hand player's HUD (#345): the turn pace bar and the Controls panel. What they show
// is decided in `handView.ts` and `controls.ts`; `screen.ts` places them and feeds them.

import type { PlayTurn } from "../../play";
import { describePace } from "../lobby/form";
import { controlParts, controlRows, pointerRows } from "./controls";
import { paceSegment } from "./handView";

let panels = 0;

function reducedMotion(): MediaQueryList | null {
  return typeof matchMedia === "function"
    ? matchMedia("(prefers-reduced-motion: reduce)")
    : null;
}

/**
 * A thin bar beside the turn that fills over each turn, from the turn's pace and end time. It is
 * a Web Animation, so no script runs per frame; with reduced motion it is the words "1 s a turn".
 */
export class PaceBar {
  readonly element = document.createElement("span");
  private readonly fill = document.createElement("span");
  private readonly words = document.createElement("span");
  private readonly motion = reducedMotion();
  private turn: PlayTurn | null = null;
  /** The turn the bar is drawn for: a new one restarts it. */
  private drawn = "";
  private readonly onMotion = () => {
    this.drawn = "";
    this.show(this.turn);
  };

  constructor() {
    this.element.className = "gv-pace";
    this.element.setAttribute("role", "img");
    this.fill.className = "gv-pace-fill";
    this.words.className = "gv-pace-words";
    this.element.append(this.fill, this.words);
    this.element.hidden = true;
    this.motion?.addEventListener("change", this.onMotion);
  }

  /** Shows the bar for the open turn, or hides it for null. */
  show(turn: PlayTurn | null): void {
    this.turn = turn;
    const segment = turn && paceSegment(turn, Date.now());
    if (!turn || !segment || turn.paceMs === null) {
      this.element.hidden = true;
      this.drawn = "";
      this.stop();
      return;
    }
    this.element.hidden = false;
    const reduced = this.motion?.matches === true;
    const key = `${turn.gameId}:${turn.generationId}:${turn.endsAt}:${reduced}`;
    if (key === this.drawn) return;
    this.drawn = key;
    const pace = describePace(turn.paceMs);
    this.element.setAttribute("aria-label", `A turn every ${pace}`);
    this.element.dataset.motion = reduced ? "reduced" : "full";
    this.words.textContent = reduced ? `${pace} a turn` : "";
    this.stop();
    if (reduced || typeof this.fill.animate !== "function") return;
    this.fill.animate(
      [{ transform: `scaleX(${segment.from})` }, { transform: "scaleX(1)" }],
      { duration: segment.durationMs, easing: "linear", fill: "forwards" },
    );
  }

  dispose(): void {
    this.show(null);
    this.motion?.removeEventListener("change", this.onMotion);
  }

  private stop(): void {
    if (typeof this.fill.getAnimations !== "function") return;
    for (const animation of this.fill.getAnimations()) animation.cancel();
  }
}

/**
 * The Controls button of the top bar and the panel it opens: a disclosure, not a dialog, so the
 * board stays usable beside it. `Esc` closes it while the focus is in it; outside it the key
 * belongs to the board's controls.
 */
export class ControlsPanel {
  readonly button = document.createElement("button");
  readonly panel = document.createElement("section");
  private readonly body = document.createElement("tbody");
  private readonly switcher = document.createElement("button");
  private touch = false;

  constructor() {
    const id = `gv-controls-${++panels}`;
    this.button.type = "button";
    this.button.className = "gv-button";
    this.button.textContent = "Controls";
    this.button.setAttribute("aria-expanded", "false");
    this.button.setAttribute("aria-controls", id);
    this.button.hidden = true;
    this.button.addEventListener("click", () => {
      if (this.panel.hidden) this.open(true);
      else this.close();
    });

    this.panel.id = id;
    this.panel.className = "gv-controls";
    this.panel.setAttribute("aria-labelledby", `${id}-title`);
    this.panel.hidden = true;
    const header = document.createElement("header");
    const title = document.createElement("h2");
    title.id = `${id}-title`;
    title.textContent = "Controls";
    const close = document.createElement("button");
    close.type = "button";
    close.className = "gv-button";
    close.textContent = "Close";
    close.addEventListener("click", () => this.close());
    header.append(title, close);
    const table = document.createElement("table");
    table.append(this.body);
    this.switcher.type = "button";
    this.switcher.className = "gv-controls-switch";
    this.switcher.addEventListener("click", () => {
      this.touch = !this.touch;
      this.renderRows(true);
    });
    this.panel.append(header, table, this.switcher);
    this.panel.addEventListener("keydown", (event) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      this.close();
    });
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  /** Opens the panel; `focus` moves the focus into it, as a click on the button does. */
  open(focus: boolean): void {
    const pointers = pointerRows((query) =>
      typeof matchMedia === "function" ? matchMedia(query).matches : false,
    );
    this.touch = pointers.touch;
    this.switcher.hidden = !pointers.canSwitch;
    this.renderRows(false);
    this.panel.hidden = false;
    this.button.setAttribute("aria-expanded", "true");
    if (focus)
      this.panel.querySelector<HTMLButtonElement>("header button")?.focus();
  }

  close(): void {
    if (this.panel.hidden) return;
    const hadFocus = this.panel.contains(document.activeElement);
    this.panel.hidden = true;
    this.button.setAttribute("aria-expanded", "false");
    if (hadFocus) this.button.focus();
  }

  private renderRows(keepFocus: boolean): void {
    const rows = controlRows(this.touch).map(({ action, how }) => {
      const row = document.createElement("tr");
      const name = document.createElement("th");
      name.scope = "row";
      name.textContent = action;
      const cell = document.createElement("td");
      for (const part of controlParts(how)) {
        if (part.key) {
          const key = document.createElement("kbd");
          key.textContent = part.text;
          cell.append(key);
        } else cell.append(part.text);
      }
      row.append(name, cell);
      return row;
    });
    this.body.replaceChildren(...rows);
    this.switcher.textContent = this.touch
      ? "Show mouse and keyboard controls"
      : "Show touch controls";
    if (keepFocus) this.switcher.focus();
  }
}
