// The map builder (`/map/builder/`, backend/mapbuilder/) opened from the create form's map
// choice. Its "Use in lobby" button posts the map file's text to the page that opened it. This is
// the 3D client's side of what backend/demo/lobby-settings.mjs does for the lobby page, with the
// same message types, window name and checks.

import { isDesktop } from "../../platform";

/** The message type the builder's Use in lobby button posts; builder.mjs sends it. */
export const mapMessageType = "island-battles.map-file";
/** What a form tells a builder that outlived a reload of the form: send maps to me. */
export const lobbyPageMessageType = "island-battles.lobby-page";
export const builderPath = "/map/builder/";
const builderWindowName = "island-battles-map-builder";

/**
 * The builder hands its map back only to a window of its own origin. The web client is served
 * by the game server (or proxied to it in development), so it is; the desktop app's window has
 * another origin (see `src/platform.ts`), so the choice is not offered there.
 */
export const mapBuilderAvailable = !isDesktop;

export interface BuilderMessage {
  origin: string;
  source: unknown;
  data: unknown;
}

/**
 * The map file text a message carries, or null when the message is not the builder's: from
 * another origin or window, or of another type. A message that is the builder's but holds no
 * text throws, like a bad file does. `builder` is the window the form opened.
 */
export function mapTextFromBuilderMessage(
  event: BuilderMessage,
  context: { origin: string; builder: unknown },
): string | null {
  if (
    !context.builder ||
    event.origin !== context.origin ||
    event.source !== context.builder
  ) {
    return null;
  }
  const data = event.data as { type?: unknown; text?: unknown } | null;
  if (data?.type !== mapMessageType) return null;
  if (typeof data.text !== "string") {
    throw new Error("The map builder sent no map file.");
  }
  return data.text;
}

/**
 * What to do with the window `window.open("", name)` returned. One already on the builder is
 * kept, because navigating it would reload the builder and lose its unsaved map; anything else,
 * such as a fresh blank window, is sent to the builder.
 */
export function builderWindowAction(
  opened: Pick<Window, "location">,
  origin: string,
): "keep" | "navigate" {
  try {
    if (
      opened.location.origin === origin &&
      opened.location.pathname.startsWith(builderPath)
    ) {
      return "keep";
    }
  } catch {
    // A window of another origin can't be read; take it over.
  }
  return "navigate";
}

export interface MapBuilderLink {
  /** Opens the builder in its own tab, or focuses the one already open. */
  open(): void;
  dispose(): void;
}

/**
 * Opens the builder and listens for its maps. `onMapText` gets the text of each map the builder
 * sends; `onProblem` a sentence for a message that holds no map. It must not open with
 * `noopener`, because the builder reaches this page through `window.opener`.
 */
export function createMapBuilderLink(handlers: {
  onMapText(text: string): void;
  onProblem(message: string): void;
}): MapBuilderLink {
  let builder: Window | null = null;
  const receive = (event: MessageEvent): void => {
    try {
      const text = mapTextFromBuilderMessage(event, {
        origin: location.origin,
        builder,
      });
      if (text !== null) handlers.onMapText(text);
    } catch (error) {
      handlers.onProblem(error instanceof Error ? error.message : "");
    }
  };
  window.addEventListener("message", receive);
  return {
    open() {
      if (builder && !builder.closed) {
        builder.focus();
        return;
      }
      // After this page reloads, it has forgotten the window but the browser has not, so it
      // asks for the window by name without a URL and then decides.
      const opened = window.open("", builderWindowName);
      if (!opened) return;
      builder = opened;
      if (builderWindowAction(opened, location.origin) === "keep") {
        // Its opener is the page before the reload, so name this page instead.
        opened.postMessage({ type: lobbyPageMessageType }, location.origin);
        opened.focus();
      } else {
        opened.location.href = builderPath;
      }
    },
    dispose() {
      window.removeEventListener("message", receive);
    },
  };
}
