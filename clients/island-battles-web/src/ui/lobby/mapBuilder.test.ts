import { describe, expect, test } from "bun:test";
import {
  builderWindowAction,
  mapMessageType,
  mapTextFromBuilderMessage,
} from "./mapBuilder";

const builder: object = {};
const origin = "http://localhost:3000";
const context = { origin, builder };

describe("mapTextFromBuilderMessage", () => {
  const message = { type: mapMessageType, text: '{"map_name":"x"}' };

  test("takes the text of the builder's message", () => {
    expect(
      mapTextFromBuilderMessage(
        { origin, source: builder, data: message },
        context,
      ),
    ).toBe('{"map_name":"x"}');
  });

  test("ignores another origin, another window, another type and no builder", () => {
    const from = (
      event: object,
      ctx: { origin: string; builder: unknown } = context,
    ) =>
      mapTextFromBuilderMessage(
        { origin, source: builder, data: message, ...event },
        ctx,
      );
    expect(from({ origin: "https://evil.example" })).toBeNull();
    expect(from({ source: {} })).toBeNull();
    expect(from({ data: { type: "other", text: "{}" } })).toBeNull();
    expect(from({ data: null })).toBeNull();
    expect(from({}, { origin, builder: null })).toBeNull();
  });

  test("a builder message without text is a problem", () => {
    expect(() =>
      mapTextFromBuilderMessage(
        { origin, source: builder, data: { type: mapMessageType } },
        context,
      ),
    ).toThrow("The map builder sent no map file.");
  });
});

describe("builderWindowAction", () => {
  const at = (originOfWindow: string, pathname: string) => ({
    location: { origin: originOfWindow, pathname } as Location,
  });

  test("keeps a window already on the builder, so its map is not lost", () => {
    expect(builderWindowAction(at(origin, "/map/builder/"), origin)).toBe(
      "keep",
    );
  });

  test("sends a blank or foreign window to the builder", () => {
    expect(builderWindowAction(at(origin, "/"), origin)).toBe("navigate");
    expect(
      builderWindowAction(at("https://other.example", "/map/builder/"), origin),
    ).toBe("navigate");
    const foreign = {
      get location(): Location {
        throw new DOMException("blocked", "SecurityError");
      },
    };
    expect(builderWindowAction(foreign, origin)).toBe("navigate");
  });
});
