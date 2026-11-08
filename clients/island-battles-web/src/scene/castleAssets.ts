import atlasUrl from "../../assets/castles/atlas.png";
import level1Url from "../../assets/castles/castle-level-1.glb";
import level2Url from "../../assets/castles/castle-level-2.glb";
import level3Url from "../../assets/castles/castle-level-3.glb";
import level4Url from "../../assets/castles/castle-level-4.glb";

// The castle models and their atlas (issue #140), made by tools/castles/convert.py. Importing the
// files gives their URLs: the build copies them under /play/assets/ with hashed names, and the
// desktop build serves them from its own origin.

/** The GLB of each castle level, 1 to 4. */
export const CASTLE_MODEL_URLS: readonly string[] = [
  level1Url,
  level2Url,
  level3Url,
  level4Url,
];

export const CASTLE_ATLAS_URL: string = atlasUrl;
