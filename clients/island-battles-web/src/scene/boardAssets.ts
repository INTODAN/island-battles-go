import type { Scene } from "@babylonjs/core/scene";
import {
  type AssetProgress,
  assetProgress,
  fetchAsset,
  onAssetProgress,
} from "./assetLoader";
import { CASTLE_ATLAS_URL, CASTLE_MODEL_URLS } from "./castleAssets";
import { loadCastleModels } from "./castles";
import { createBoardLook, SKY_URL } from "./look";

// The board's files (issue #102): the castle atlas and models (#140) and the environment
// texture. They load lazily, once a board is shown: the lobby screens appear before them, and a
// board shows a loading screen with their progress until they are in.

export const BOARD_ASSET_URLS: readonly string[] = [
  CASTLE_ATLAS_URL,
  ...CASTLE_MODEL_URLS,
  SKY_URL,
];

export type { AssetProgress };

/**
 * Loads the board's files into `scene`, calling `onProgress` as bytes arrive. It resolves when
 * the castles and environment texture are ready, and rejects if the castles fail (the board then
 * stays usable with its placeholder art).
 */
export async function loadBoardAssets(
  scene: Scene,
  onProgress?: (progress: AssetProgress) => void,
): Promise<void> {
  const report = () => onProgress?.(assetProgress(BOARD_ASSET_URLS));
  const stop = onAssetProgress(report);
  // Every download starts at once.
  for (const url of BOARD_ASSET_URLS) fetchAsset(url).catch(() => {});
  report();
  try {
    await Promise.all([
      loadCastleModels(scene),
      createBoardLook(scene).skyReady,
    ]);
  } finally {
    stop();
    report();
  }
}
