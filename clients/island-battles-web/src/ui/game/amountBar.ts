// The amount bar of drag to send (#343): a slider between a Half and an All button, above the
// timeline. The board's controls draw it (`render`) from the drag model and hear its changes;
// before a drag it shows the share the next drag sends ("Send 50%"), during one the origin's
// soldiers ("12 of 23").

import {
  ALL,
  type AmountControl,
  type DragAmount,
  HALF,
  type SliderView,
  sliderAmount,
} from "../../play/drag";

const HALF_ICON =
  '<svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="8" fill="none" stroke="currentColor" stroke-width="2"/><path d="M12 4a8 8 0 0 0 0 16z" fill="currentColor"/></svg>';
const ALL_ICON =
  '<svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="8" fill="currentColor" stroke="currentColor" stroke-width="2"/></svg>';

function iconButton(
  label: string,
  title: string,
  icon: string,
  onPress: () => void,
) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "gv-amount-button";
  button.setAttribute("aria-label", label);
  button.title = title;
  button.innerHTML = icon;
  // A press on it never takes the focus from the board: Babylon's canvas `blur` listener makes up a
  // release for every pressed pointer, which would drop a drag a finger holds while another taps
  // here. It also keeps the drag's keys working.
  button.addEventListener("pointerdown", (event) => event.preventDefault());
  // A browser fires `click` for a finger that is the only one down, not for a second one: a tap
  // of a second finger, while the first holds a drag, is `pointerdown` and `pointerup` alone. So
  // that tap presses the button on its `pointerup`. A touch stays attached to the button it
  // pressed, so only a lift inside the button counts, as a click's would. A `click` some browser
  // still adds after the tap (a pointer's, `detail > 0`) is skipped once; a keyboard's or an
  // assistive technology's (`detail` 0) never is.
  const secondFingers = new Set<number>();
  let skipNextClick = false;
  button.addEventListener("pointerdown", (event) => {
    skipNextClick = false;
    if (event.pointerType === "touch" && !event.isPrimary) {
      secondFingers.add(event.pointerId);
    }
  });
  button.addEventListener("pointerup", (event) => {
    if (!secondFingers.delete(event.pointerId)) return;
    const box = button.getBoundingClientRect();
    const inside =
      event.clientX >= box.left &&
      event.clientX <= box.right &&
      event.clientY >= box.top &&
      event.clientY <= box.bottom;
    if (!inside) return;
    skipNextClick = true;
    onPress();
  });
  button.addEventListener("pointercancel", (event) => {
    secondFingers.delete(event.pointerId);
  });
  button.addEventListener("click", (event) => {
    const skip = skipNextClick && event.detail > 0;
    skipNextClick = false;
    if (skip) return;
    onPress();
  });
  return button;
}

/**
 * What a screen reader is told when the bar's view changes: the drag starting (with the amount it
 * sends) or ending, never the steps of `w`, `s` and the slider in between. Null for no news.
 */
export function announcement(
  before: SliderView | null,
  after: SliderView | null,
): string | null {
  const dragging = (view: SliderView | null) => view?.mode === "soldiers";
  if (dragging(after) && !dragging(before)) {
    return `Sending ${after?.text ?? ""}`;
  }
  if (dragging(before) && !dragging(after)) return "Drag ended";
  return null;
}

export class AmountBar implements AmountControl {
  readonly element = document.createElement("div");
  private readonly slider = document.createElement("input");
  private readonly text = document.createElement("output");
  /**
   * Tells a drag's start and end to screen readers (`announcement`). It stands beside `element`,
   * not inside it, so the end of a drag is still announced when `render` hides the bar; whoever
   * adds `element` to the page adds this too.
   */
  readonly status = document.createElement("span");
  private readonly listeners = new Set<(amount: DragAmount) => void>();
  private view: SliderView | null = null;

  constructor() {
    const { element, slider, text, status } = this;
    element.className = "gv-amount";
    element.setAttribute("role", "group");
    element.setAttribute("aria-label", "Soldiers to send");
    element.hidden = true;
    slider.type = "range";
    slider.className = "gv-amount-slider";
    // A name of its own, not the group's.
    slider.setAttribute("aria-label", "Amount");
    text.className = "gv-amount-text";
    // The slider's value text is read on a step; the output beside it must not announce every
    // step a second time.
    text.setAttribute("aria-live", "off");
    status.className = "gv-visually-hidden";
    status.setAttribute("role", "status");
    const half = iconButton("Half", "Half (A)", HALF_ICON, () =>
      this.emit(HALF),
    );
    const all = iconButton("All", "All (D)", ALL_ICON, () => this.emit(ALL));
    // A finger on the slider keeps the focus on the board, for the reason the buttons do. Only a
    // touch: cancelling a mouse's or pen's pointerdown stops the browser dragging the slider, and
    // a mouse cannot hold a drag with one hand and move the slider with another anyway. A touch
    // still drags the slider (the browser reads it from the touch events), and the keyboard still
    // moves it once it has the focus (Tab).
    slider.addEventListener("pointerdown", (event) => {
      if (event.pointerType === "touch") event.preventDefault();
    });
    slider.addEventListener("input", () => {
      if (this.view) this.emit(sliderAmount(this.view, Number(slider.value)));
    });
    element.append(half, slider, all, text);
  }

  render(view: SliderView | null): void {
    const told = announcement(this.view, view);
    if (told !== null) this.status.textContent = told;
    this.view = view;
    this.element.hidden = view === null;
    if (!view) return;
    const { slider } = this;
    slider.min = String(view.min);
    slider.max = String(view.max);
    if (slider.value !== String(view.value)) slider.value = String(view.value);
    slider.disabled = view.disabled;
    slider.setAttribute("aria-valuetext", view.text);
    this.text.textContent = view.text;
  }

  onChange(listener: (amount: DragAmount) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  private emit(amount: DragAmount): void {
    for (const listener of [...this.listeners]) listener(amount);
  }
}
