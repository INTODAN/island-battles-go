import { describe, expect, test } from "bun:test";
import { fakeClock } from "../../play/testing";
import {
  createOrderNotices,
  ORDER_FEEDBACK_MS,
  orderNoticeText,
} from "./notices";

function setup() {
  const clock = fakeClock();
  const shown: (string | null)[] = [];
  const notices = createOrderNotices({
    render: (text) => shown.push(text),
    schedule: clock.schedule,
  });
  return { clock, shown, notices, last: () => shown.at(-1) };
}

describe("order feedback", () => {
  test("a sentence shows for 5 s, then goes", async () => {
    const { clock, notices, last } = setup();
    notices.show("The castle has 3 soldiers; it can't send 5.", 1);
    expect(last()).toBe("The castle has 3 soldiers; it can't send 5.");
    await clock.advance(ORDER_FEEDBACK_MS - 1);
    expect(last()).toBe("The castle has 3 soldiers; it can't send 5.");
    await clock.advance(1);
    expect(last()).toBeNull();
    expect(clock.pending()).toBe(0);
  });

  test("the newest sentence shows, with a count from the second order, and each restarts the time", async () => {
    const { clock, notices, last } = setup();
    notices.show("First.", 1);
    await clock.advance(4000);
    notices.show("The turn ended before the order arrived.", 2);
    notices.show("The turn ended before the order arrived.", 3);
    expect(last()).toBe(
      "The turn ended before the order arrived. (3 orders refused)",
    );
    await clock.advance(4000);
    expect(last()).toBe(
      "The turn ended before the order arrived. (3 orders refused)",
    );
    await clock.advance(1000);
    expect(last()).toBeNull();
    // A new window counts from one again.
    notices.show("Again.", 4);
    expect(last()).toBe("Again.");
  });

  test("one order is shown once, whoever reports it", async () => {
    const { clock, notices, shown } = setup();
    notices.show("Refused.", 5);
    notices.show("Refused.", 5);
    expect(shown).toEqual(["Refused."]);
    // A second report doesn't restart the time.
    await clock.advance(ORDER_FEEDBACK_MS);
    expect(shown.at(-1)).toBeNull();
  });

  test("a sentence without an id counts each time, so a repeat is announced", () => {
    const { notices, last } = setup();
    notices.show("The castle has no soldiers to send.");
    expect(last()).toBe("The castle has no soldiers to send.");
    notices.show("The castle has no soldiers to send.");
    expect(last()).toBe(
      "The castle has no soldiers to send. (2 orders refused)",
    );
    notices.show("A refused resignation.");
    expect(last()).toBe("A refused resignation. (3 orders refused)");
  });

  test("dispose hides the line and leaves no timer", () => {
    const { clock, notices, last } = setup();
    notices.show("Refused.", 1);
    notices.dispose();
    expect(last()).toBeNull();
    expect(clock.pending()).toBe(0);
  });

  test("the text", () => {
    expect(orderNoticeText("No.", 1)).toBe("No.");
    expect(orderNoticeText("No.", 2)).toBe("No. (2 orders refused)");
  });
});
