// Saving the game so far as a demo file, which Open demo file (and the 2D demo viewer) opens
// again. The file is the server's own answer, saved as it came.

import type { createApiClients } from "../../api/client";

type GameApi = ReturnType<typeof createApiClients>["gameApi"];

export interface DemoDownload {
  fileName: string;
  /** The demo file's text, as the server sent it. */
  text: string;
}

/** The file name in a `Content-Disposition` header, or undefined. Never a path. */
export function fileNameFromDisposition(
  header: string | null | undefined,
): string | undefined {
  if (!header) return undefined;
  const encoded = /filename\*\s*=\s*UTF-8''([^;]+)/i.exec(header)?.[1];
  let name: string | undefined;
  if (encoded) {
    try {
      name = decodeURIComponent(encoded.trim());
    } catch {
      name = undefined;
    }
  }
  name ??=
    /filename\s*=\s*"([^"]*)"/i.exec(header)?.[1] ??
    /filename\s*=\s*([^;]+)/i.exec(header)?.[1]?.trim();
  if (!name || /[/\\]/.test(name)) return undefined;
  return name;
}

/** The server's name pattern, for when the header can't be read (`DemoFileName` in demo.go). */
export function fallbackFileName(text: string): string {
  try {
    const demo = JSON.parse(text) as { game_id?: unknown; frames?: unknown };
    if (Number.isSafeInteger(demo.game_id) && Array.isArray(demo.frames)) {
      return `island-battles-game-${demo.game_id}-generation-${demo.frames.length}.json`;
    }
  } catch {
    // Name it generically below.
  }
  return "island-battles-demo.json";
}

/**
 * Downloads the game so far: a roster player's `/demo`, or a watcher's `/watch/demo`. The API
 * client throws an `ApiError` for a refusal or a lost connection, and retries a throttled read.
 */
export async function downloadDemo(
  gameApi: GameApi,
  lobbyId: string,
  watching: boolean,
): Promise<DemoDownload> {
  const params = { path: { lobby_id: lobbyId } };
  const { data, response } = watching
    ? await gameApi.GET("/game/from-lobby/{lobby_id}/watch/demo", {
        params,
        parseAs: "text",
      })
    : await gameApi.GET("/game/from-lobby/{lobby_id}/demo", {
        params,
        parseAs: "text",
      });
  const text = data as unknown as string;
  return {
    fileName:
      fileNameFromDisposition(response.headers.get("Content-Disposition")) ??
      fallbackFileName(text),
    text,
  };
}

/** Hands a file to the browser with a temporary download link. */
export function saveTextFile(download: DemoDownload): void {
  const url = URL.createObjectURL(
    new Blob([download.text], { type: "application/json" }),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = download.fileName;
  link.hidden = true;
  document.body.append(link);
  link.click();
  link.remove();
  // The download has read the file long before this.
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}
