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

function iconButton(label: string, title: string, icon: string) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "gv-amount-button";
  button.setAttribute("aria-label", label);
  button.title = title;
  button.innerHTML = icon;
  // A tap on it never takes the focus from the board, so the drag's keys keep working.
  button.addEventListener("pointerdown", (event) => event.preventDefault());
  return button;
}

export class AmountBar implements AmountControl {
  readonly element = document.createElement("div");
  private readonly slider = document.createElement("input");
  private readonly text = document.createElement("output");
  private readonly listeners = new Set<(amount: DragAmount) => void>();
  private view: SliderView | null = null;

  constructor() {
    const { element, slider, text } = this;
    element.className = "gv-amount";
    element.setAttribute("role", "group");
    element.setAttribute("aria-label", "Soldiers to send");
    element.hidden = true;
    slider.type = "range";
    slider.className = "gv-amount-slider";
    slider.setAttribute("aria-label", "Soldiers to send");
    text.className = "gv-amount-text";
    const half = iconButton("Half", "Half (A)", HALF_ICON);
    const all = iconButton("All", "All (D)", ALL_ICON);
    half.addEventListener("click", () => this.emit(HALF));
    all.addEventListener("click", () => this.emit(ALL));
    slider.addEventListener("input", () => {
      if (this.view) this.emit(sliderAmount(this.view, Number(slider.value)));
    });
    element.append(half, slider, all, text);
  }

  render(view: SliderView | null): void {
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
