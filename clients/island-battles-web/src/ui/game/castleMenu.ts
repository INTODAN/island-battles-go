// The castle menu (#344): a hand player buys upgrades and orders bursts for one of their castles.
// It opens beside the castle (a right-click, a double-click or a double-tap, `castleMenuInput.ts`),
// follows it on screen, and draws `castleMenuModel` from `src/play/`, which holds every rule. It is a
// non-modal dialog in the game view's DOM overlay: Tab moves through its rows, Esc closes it and
// gives the focus back. Every value is written with `textContent`.

import {
  burstAmount,
  type CastleMenuModel,
  castleMenuModel,
  type HandPlay,
  isRightClick,
  type MenuOpener,
  menuAllowed,
  menuUpgradeOrder,
  type OrderResult,
  placeMenu,
  type UpgradeRow,
  type UpgradeType,
} from "../../play";

export interface CastleMenuOptions {
  /** The game view's root: the menu is placed in it, over the canvas, which fills it. */
  host: HTMLElement;
  handPlay: HandPlay;
  /** Whether the view shows the newest turn: the menu opens and stays open only then. */
  showsNewestTurn(): boolean;
  /** Hears every change of the view; returns the unsubscribe. */
  subscribeView(listener: () => void): () => void;
  /** The castle's place on screen, in CSS pixels from the host's top left, or null when it isn't drawn. */
  locate(castleId: number): { x: number; y: number } | null;
  /** Runs `run` after every frame the board draws; returns the remover. */
  onFrame(run: () => void): () => void;
  /** Where the focus goes back to when the menu closes and nothing else had it: the canvas. */
  fallbackFocus: HTMLElement;
  /**
   * Shows a refused or rejected order's sentence in the game view's order feedback line
   * (`HandSurface.showOrderFeedback`), which announces it; the menu shows it too, as plain text.
   */
  showOrderFeedback(sentence: string, orderId?: number): void;
}

function element<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const created = document.createElement(tag);
  if (className) created.className = className;
  if (text !== undefined) created.textContent = text;
  return created;
}

let nextMenuId = 1;

interface RowParts {
  button: HTMLButtonElement;
  name: HTMLElement;
  cost: HTMLElement;
  effect: HTMLElement;
  description: HTMLElement;
  ordered: HTMLElement;
  reason: HTMLElement;
}

const soldiersText = (count: number) =>
  count === 1 ? "1 soldier" : `${count} soldiers`;

export class CastleMenu {
  private readonly root = element("div", "gv-castle-menu");
  private readonly title = element("h2", "gv-cm-title");
  private readonly subtitle = element("p", "gv-cm-subtitle");
  private readonly closeButton = element("button", "gv-cm-close", "×");
  private readonly rows = new Map<UpgradeType, RowParts>();
  private readonly burstSummary = element("p", "gv-cm-burst-summary");
  private readonly burstSlider = element("input", "gv-cm-slider");
  private readonly burstButton = element("button", "gv-cm-order");
  private readonly burstReason = element("p", "gv-cm-reason");
  private readonly error = element("p", "gv-cm-error");
  private castleId: number | null = null;
  private model: CastleMenuModel | null = null;
  /** The burst the player chose with the slider, or null for the largest. */
  private burstWanted: number | null = null;
  /** What had the focus before the menu opened. */
  private returnFocus: HTMLElement | null = null;
  /** A press outside the menu, to close it if it doesn't drag. */
  private outsideDown: { x: number; y: number; pointerId: number } | null =
    null;
  private placed = { left: Number.NaN, top: Number.NaN };
  private readonly removers: (() => void)[] = [];
  /** Set by `dispose`: an order that settles later shows nothing. */
  private disposed = false;

  constructor(private readonly options: CastleMenuOptions) {
    const id = `gv-cm-${nextMenuId++}`;
    const root = this.root;
    root.hidden = true;
    root.tabIndex = -1;
    root.setAttribute("role", "dialog");
    this.title.id = `${id}-title`;
    this.subtitle.id = `${id}-subtitle`;
    root.setAttribute("aria-labelledby", this.title.id);
    root.setAttribute("aria-describedby", this.subtitle.id);

    const head = element("div", "gv-cm-head");
    this.closeButton.type = "button";
    this.closeButton.setAttribute("aria-label", "Close the castle menu");
    this.closeButton.addEventListener("click", () => this.close());
    const heading = element("div");
    heading.append(this.title, this.subtitle);
    head.append(heading, this.closeButton);

    const list = element("div", "gv-cm-rows");
    for (const type of menuUpgradeOrder) {
      const parts: RowParts = {
        button: element("button", "gv-cm-row"),
        name: element("span", "gv-cm-name"),
        cost: element("span", "gv-cm-cost"),
        effect: element("span", "gv-cm-effect"),
        description: element("span", "gv-cm-description"),
        ordered: element("span", "gv-cm-ordered"),
        reason: element("span", "gv-cm-reason"),
      };
      parts.button.type = "button";
      parts.button.dataset.type = type;
      const line = element("span", "gv-cm-line");
      line.append(parts.name, parts.cost);
      parts.button.append(
        line,
        parts.effect,
        parts.description,
        parts.ordered,
        parts.reason,
      );
      parts.button.addEventListener("click", () => this.buy(type));
      this.rows.set(type, parts);
      list.append(parts.button);
    }

    const burst = element("div", "gv-cm-burst");
    const burstName = element("span", "gv-cm-name", "Order a burst");
    this.burstSlider.type = "range";
    this.burstSlider.min = "1";
    this.burstSlider.step = "1";
    this.burstSlider.setAttribute("aria-label", "Burst soldiers");
    this.burstSlider.addEventListener("input", () => {
      this.burstWanted = Number(this.burstSlider.value);
      this.render();
    });
    this.burstButton.type = "button";
    this.burstButton.addEventListener("click", () => this.orderBurst());
    // The burst's potential and, when it can't be ordered, why: read with the slider and the button.
    this.burstSummary.id = `${id}-burst-summary`;
    this.burstReason.id = `${id}-burst-reason`;
    const burstDescription = `${this.burstSummary.id} ${this.burstReason.id}`;
    this.burstSlider.setAttribute("aria-describedby", burstDescription);
    this.burstButton.setAttribute("aria-describedby", burstDescription);
    const controls = element("div", "gv-cm-burst-controls");
    controls.append(this.burstSlider, this.burstButton);
    burst.append(burstName, this.burstSummary, controls, this.burstReason);

    // Plain text: the order feedback line announces the sentence.
    this.error.hidden = true;
    root.append(head, list, burst, this.error);
    options.host.append(root);

    const onKey = (event: KeyboardEvent) => {
      // A drag to send (#343) takes Esc first, in the capture phase, and marks it handled.
      if (
        event.key !== "Escape" ||
        event.defaultPrevented ||
        this.castleId === null
      ) {
        return;
      }
      event.preventDefault();
      this.close();
    };
    // Captured, so a press reaches the menu before the board's controls act on it.
    const onDown = (event: PointerEvent) => {
      // A second finger while one is down (a pinch) is no click outside.
      if (this.outsideDown && this.outsideDown.pointerId !== event.pointerId) {
        this.outsideDown = null;
        return;
      }
      this.outsideDown =
        this.castleId !== null && !root.contains(event.target as Node)
          ? { x: event.clientX, y: event.clientY, pointerId: event.pointerId }
          : null;
    };
    const onUp = (event: PointerEvent) => {
      const down = this.outsideDown;
      this.outsideDown = null;
      // A click or tap outside closes the menu; a drag outside (turning the camera) doesn't.
      if (
        down &&
        down.pointerId === event.pointerId &&
        isRightClick(down, { x: event.clientX, y: event.clientY })
      ) {
        this.close();
      }
    };
    const onCancel = () => {
      this.outsideDown = null;
    };
    window.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onDown, true);
    document.addEventListener("pointerup", onUp, true);
    document.addEventListener("pointercancel", onCancel, true);
    this.removers.push(
      () => window.removeEventListener("keydown", onKey),
      () => document.removeEventListener("pointerdown", onDown, true),
      () => document.removeEventListener("pointerup", onUp, true),
      () => document.removeEventListener("pointercancel", onCancel, true),
      options.handPlay.state.subscribe(() => this.update()),
      options.subscribeView(() => this.update()),
      options.onFrame(() => this.place()),
    );
  }

  /** The castle the menu is open on, or null. */
  get openCastle(): number | null {
    return this.castleId;
  }

  /** Whether the menu may open on `castleId` now. */
  allowed(castleId: number): boolean {
    const state = this.options.handPlay.state.get();
    return menuAllowed(
      {
        status: state.status,
        showsNewestTurn: this.options.showsNewestTurn(),
        castles: state.castles,
      },
      castleId,
    );
  }

  /** Opens the menu on `castleId` (closing it on any other castle) if it may; returns whether it did. */
  open(castleId: number, _opener: MenuOpener): boolean {
    if (!this.allowed(castleId)) return false;
    if (this.castleId !== castleId) {
      const active = document.activeElement;
      const fromInside = active instanceof Node && this.root.contains(active);
      if (!fromInside) {
        this.returnFocus = active instanceof HTMLElement ? active : null;
      }
      this.castleId = castleId;
      this.burstWanted = null;
      this.showError(null);
    }
    // The press that opened it isn't a click outside.
    this.outsideDown = null;
    this.root.hidden = false;
    this.placed = { left: Number.NaN, top: Number.NaN };
    this.render();
    this.place();
    this.rows.get(menuUpgradeOrder[0] as UpgradeType)?.button.focus();
    return true;
  }

  close(): void {
    if (this.castleId === null) return;
    const active = document.activeElement;
    const hadFocus = active instanceof Node && this.root.contains(active);
    this.castleId = null;
    this.model = null;
    this.root.hidden = true;
    this.showError(null);
    if (hadFocus) {
      const back =
        this.returnFocus?.isConnected && this.returnFocus !== document.body
          ? this.returnFocus
          : this.options.fallbackFocus;
      back.focus({ preventScroll: true });
    }
    this.returnFocus = null;
  }

  dispose(): void {
    this.disposed = true;
    for (const remove of this.removers.splice(0)) remove();
    this.root.remove();
  }

  /** Closes the menu when it may no longer be open, or redraws it. */
  /** Closes the menu if it may no longer be open, else draws it again. */
  update(): void {
    const castleId = this.castleId;
    if (castleId === null) return;
    if (!this.allowed(castleId)) {
      this.close();
      return;
    }
    const active = document.activeElement;
    const hadFocus = active instanceof Node && this.root.contains(active);
    this.render();
    // The slider is disabled once the castle can't burst: keep the focus in the menu.
    if (
      hadFocus &&
      !(
        document.activeElement instanceof Node &&
        this.root.contains(document.activeElement)
      )
    ) {
      this.burstButton.focus({ preventScroll: true });
    }
  }

  private buy(type: UpgradeType): void {
    const castleId = this.castleId;
    const row = this.model?.upgrades.find((entry) => entry.type === type);
    if (castleId === null || !row || row.disabled) return;
    this.follow(castleId, this.options.handPlay.upgrade(castleId, type));
  }

  private orderBurst(): void {
    const castleId = this.castleId;
    const burst = this.model?.burst;
    if (castleId === null || !burst || burst.disabled) return;
    const amount = burstAmount(this.burstWanted, burst.max);
    this.follow(castleId, this.options.handPlay.burst(castleId, amount));
  }

  /** Shows a refusal at once, or the server's rejection when the order settles. */
  private follow(castleId: number, result: OrderResult): void {
    if (!result.ok) {
      this.showError(result.sentence);
      this.options.showOrderFeedback(result.sentence);
      return;
    }
    this.showError(null);
    void result.settled.then((order) => {
      if (order.state !== "rejected" || this.disposed) return;
      const sentence = order.sentence ?? "The server refused the order.";
      if (this.castleId === castleId) this.showError(sentence);
      this.options.showOrderFeedback(sentence, order.id);
    });
  }

  private showError(text: string | null): void {
    this.error.textContent = text ?? "";
    this.error.hidden = text === null;
  }

  private render(): void {
    const castleId = this.castleId;
    if (castleId === null) return;
    const handPlay = this.options.handPlay;
    const model = castleMenuModel(
      { state: handPlay.state.get(), check: (spec) => handPlay.check(spec) },
      castleId,
    );
    this.model = model;
    if (!model) return;
    this.root.setAttribute("aria-label", `${model.title} menu`);
    this.title.textContent = model.title;
    this.subtitle.textContent = model.subtitle;
    for (const row of model.upgrades) this.renderRow(row);

    const burst = model.burst;
    const amount = burstAmount(this.burstWanted, burst.max);
    this.burstSummary.textContent =
      burst.potential === null
        ? "Brings soldiers in at once; the castle repays them."
        : burst.potential < 1
          ? "Brings soldiers in at once; the castle repays them. Upgrade Burst first."
          : `Up to ${soldiersText(burst.potential)} at once, in the next turn; the castle repays them from its production.`;
    this.burstSlider.max = String(Math.max(1, burst.max));
    this.burstSlider.value = String(amount);
    this.burstSlider.disabled = burst.disabled !== null;
    this.burstSlider.setAttribute("aria-valuetext", soldiersText(amount));
    this.burstButton.textContent =
      burst.disabled === null
        ? `Order burst of ${amount}`
        : burst.ordered !== null
          ? `Burst of ${burst.ordered} ordered`
          : "Order burst";
    this.setDisabled(this.burstButton, burst.disabled !== null);
    this.burstReason.textContent = burst.disabled ?? "";
    this.burstReason.hidden = burst.disabled === null;
  }

  private renderRow(row: UpgradeRow): void {
    const parts = this.rows.get(row.type);
    if (!parts) return;
    parts.name.textContent = row.name;
    parts.cost.textContent =
      row.cost === null ? "" : `${soldiersText(row.cost)}`;
    parts.effect.textContent = row.effect;
    parts.description.textContent = row.description;
    parts.ordered.textContent = row.orderedText ?? "";
    parts.ordered.hidden = row.orderedText === null;
    parts.reason.textContent = row.disabled ?? "";
    parts.reason.hidden = row.disabled === null;
    this.setDisabled(parts.button, row.disabled !== null);
  }

  /**
   * A disabled row keeps the focus and stays in the Tab order, so a screen reader still reads its
   * reason: it is marked `aria-disabled` and its click does nothing, instead of `disabled`.
   */
  private setDisabled(button: HTMLButtonElement, disabled: boolean): void {
    if (disabled) button.setAttribute("aria-disabled", "true");
    else button.removeAttribute("aria-disabled");
  }

  /** Puts the menu beside its castle, inside the board: after every frame, so it follows the camera and the turns. */
  private place(): void {
    const castleId = this.castleId;
    if (castleId === null) return;
    const anchor = this.options.locate(castleId);
    if (!anchor) return;
    const host = this.options.host;
    const { left, top } = placeMenu(
      anchor,
      { width: this.root.offsetWidth, height: this.root.offsetHeight },
      { width: host.clientWidth, height: host.clientHeight },
    );
    const roundedLeft = Math.round(left);
    const roundedTop = Math.round(top);
    if (roundedLeft === this.placed.left && roundedTop === this.placed.top) {
      return;
    }
    this.placed = { left: roundedLeft, top: roundedTop };
    this.root.style.left = `${roundedLeft}px`;
    this.root.style.top = `${roundedTop}px`;
  }
}
