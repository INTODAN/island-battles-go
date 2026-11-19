import { describe, expect, test } from "bun:test";
import type { SliderView } from "../../play/drag";
import { announcement } from "./amountBar";

const share: SliderView = {
  mode: "share",
  min: 0,
  max: 100,
  value: 50,
  text: "Send 50%",
  disabled: false,
};
const soldiers = (text: string): SliderView => ({
  mode: "soldiers",
  min: 1,
  max: 23,
  value: 12,
  text,
  disabled: false,
});

describe("what the amount bar announces", () => {
  test("a drag starting says what it sends", () => {
    expect(announcement(share, soldiers("12 of 23"))).toBe("Sending 12 of 23");
    expect(announcement(null, soldiers("12 of 23"))).toBe("Sending 12 of 23");
  });

  test("a drag ending, by a send, a cancel or leaving live, says so", () => {
    expect(announcement(soldiers("12 of 23"), share)).toBe("Drag ended");
    expect(announcement(soldiers("12 of 23"), null)).toBe("Drag ended");
  });

  test("the steps of w, s and the slider during a drag are not announced", () => {
    expect(announcement(soldiers("12 of 23"), soldiers("13 of 23"))).toBeNull();
  });

  test("changing the share between drags is not announced", () => {
    expect(announcement(share, { ...share, text: "Send all" })).toBeNull();
    expect(announcement(null, share)).toBeNull();
  });
});

/** Just enough of an element for `AmountBar`, which the tests run without a page. */
class FakeElement {
  hidden = false;
  className = "";
  textContent = "";
  innerHTML = "";
  title = "";
  type = "";
  value = "";
  children: FakeElement[] = [];
  getBoundingClientRect() {
    return { left: 100, right: 150, top: 200, bottom: 250 };
  }
  readonly listeners = new Map<string, ((event: unknown) => void)[]>();
  constructor(readonly tag: string) {}
  setAttribute() {}
  append(...children: FakeElement[]) {
    this.children.push(...children);
  }
  addEventListener(name: string, listener: (event: unknown) => void) {
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), listener]);
  }
  /** Fires a listener; returns whether the page's default was cancelled. */
  fire(name: string, init: object = {}) {
    const event = {
      ...init,
      defaultPrevented: false,
      preventDefault() {
        this.defaultPrevented = true;
      },
    };
    for (const listener of this.listeners.get(name) ?? []) listener(event);
    return event.defaultPrevented;
  }
  descendants(): FakeElement[] {
    return this.children.flatMap((child) => [child, ...child.descendants()]);
  }
}

async function barInAFakePage() {
  const globals = globalThis as Record<string, unknown>;
  const saved = globals.document;
  globals.document = {
    createElement: (tag: string) => new FakeElement(tag),
  };
  try {
    const { AmountBar } = await import("./amountBar");
    const bar = new AmountBar();
    const element = bar.element as unknown as FakeElement;
    const parts = element.descendants();
    return {
      bar,
      element,
      status: bar.status as unknown as FakeElement,
      buttons: parts.filter((part) => part.tag === "button"),
      slider: parts.find((part) => part.tag === "input") as FakeElement,
    };
  } finally {
    globals.document = saved;
  }
}

describe("the amount bar's announcements reach the screen reader", () => {
  test("a drag's end is told by a status that is outside the hidden group", async () => {
    const { bar, element, status } = await barInAFakePage();
    expect(element.descendants()).not.toContain(status);
    bar.render(soldiers("12 of 23"));
    expect(status.textContent).toBe("Sending 12 of 23");
    // The bar hides in this very call, and the end is still told.
    bar.render(null);
    expect(element.hidden).toBe(true);
    expect(status.textContent).toBe("Drag ended");
  });
});

describe("a press on the bar keeps the focus on the board", () => {
  test("a press on Half or All, of any pointer, is cancelled", async () => {
    const { buttons } = await barInAFakePage();
    expect(buttons).toHaveLength(2);
    for (const button of buttons) {
      for (const pointerType of ["mouse", "touch", "pen"]) {
        expect(button.fire("pointerdown", { pointerType })).toBe(true);
      }
    }
  });

  test("a finger on the slider is cancelled, a mouse's press is not (it would stop the dragging)", async () => {
    const { slider } = await barInAFakePage();
    expect(slider.fire("pointerdown", { pointerType: "touch" })).toBe(true);
    expect(slider.fire("pointerdown", { pointerType: "mouse" })).toBe(false);
    expect(slider.fire("pointerdown", { pointerType: "pen" })).toBe(false);
  });

  test("a click on a button and a slider's input still reach the board's controls", async () => {
    const { bar, buttons, slider } = await barInAFakePage();
    const amounts: unknown[] = [];
    bar.onChange((amount) => amounts.push(amount));
    bar.render(share);
    buttons[0]?.fire("click");
    buttons[1]?.fire("click");
    slider.value = "30";
    slider.fire("input");
    expect(amounts).toHaveLength(3);
  });
});

describe("a second finger's tap on Half or All", () => {
  const finger = (primary: boolean, id = 7) => ({
    pointerType: "touch",
    isPrimary: primary,
    pointerId: id,
    clientX: 120,
    clientY: 220,
  });

  test("presses the button on pointerup, as a browser fires no click for it, and once", async () => {
    const { bar, buttons } = await barInAFakePage();
    const amounts: unknown[] = [];
    bar.onChange((amount) => amounts.push(amount));
    const [half, all] = buttons as [FakeElement, FakeElement];
    half.fire("pointerdown", finger(false));
    expect(amounts).toEqual([]);
    half.fire("pointerup", finger(false));
    expect(amounts).toEqual([{ kind: "share", share: 0.5 }]);
    // A browser that does add a click: the same tap.
    half.fire("click", { detail: 1 });
    expect(amounts).toHaveLength(1);
    all.fire("pointerdown", finger(false, 8));
    all.fire("pointerup", finger(false, 8));
    expect(amounts.at(-1)).toEqual({ kind: "share", share: 1 });
  });

  test("a lift outside the button, where the finger slid to, presses nothing", async () => {
    const { bar, buttons } = await barInAFakePage();
    const amounts: unknown[] = [];
    bar.onChange((amount) => amounts.push(amount));
    const half = buttons[0] as FakeElement;
    for (const [clientX, clientY] of [
      [99, 220],
      [151, 220],
      [120, 199],
      [120, 251],
    ]) {
      half.fire("pointerdown", finger(false));
      half.fire("pointerup", { ...finger(false), clientX, clientY });
    }
    expect(amounts).toEqual([]);
    // The edge counts as inside.
    half.fire("pointerdown", finger(false));
    half.fire("pointerup", { ...finger(false), clientX: 100, clientY: 250 });
    expect(amounts).toHaveLength(1);
  });

  test("only the one click after the tap is skipped; a keyboard's click is not", async () => {
    const { bar, buttons } = await barInAFakePage();
    const amounts: unknown[] = [];
    bar.onChange((amount) => amounts.push(amount));
    const half = buttons[0] as FakeElement;
    // A keyboard or assistive click right after the tap (detail 0) presses.
    half.fire("pointerdown", finger(false));
    half.fire("pointerup", finger(false));
    half.fire("click", { detail: 0 });
    expect(amounts).toHaveLength(2);
    // The skip was used up: a late pointer click presses too.
    half.fire("click", { detail: 1 });
    expect(amounts).toHaveLength(3);
    // The next press clears a skip that no click used.
    half.fire("pointerdown", finger(false));
    half.fire("pointerup", finger(false));
    expect(amounts).toHaveLength(4);
    half.fire("pointerdown", finger(true, 9));
    half.fire("click", { detail: 1 });
    expect(amounts).toHaveLength(5);
  });

  test("a finger that is the only one down, or a mouse, presses on click alone", async () => {
    const { bar, buttons } = await barInAFakePage();
    const amounts: unknown[] = [];
    bar.onChange((amount) => amounts.push(amount));
    const half = buttons[0] as FakeElement;
    half.fire("pointerdown", finger(true));
    half.fire("pointerup", finger(true));
    expect(amounts).toEqual([]);
    half.fire("click", { detail: 1 });
    half.fire("click", { detail: 1 });
    expect(amounts).toHaveLength(2);
  });

  test("a second finger whose touch was cancelled presses nothing", async () => {
    const { bar, buttons } = await barInAFakePage();
    const amounts: unknown[] = [];
    bar.onChange((amount) => amounts.push(amount));
    const half = buttons[0] as FakeElement;
    half.fire("pointerdown", finger(false));
    half.fire("pointercancel", finger(false));
    half.fire("pointerup", finger(false));
    expect(amounts).toEqual([]);
  });
});
