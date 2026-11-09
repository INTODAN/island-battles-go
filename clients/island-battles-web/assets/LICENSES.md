# Asset licences

Every file under `assets/`, where it comes from and under what terms. Add a row for each new file.

The castle files are made by [`tools/castles/convert.py`](../tools/castles/convert.py) from the Unity game [INTODAN/island-battles](https://github.com/INTODAN/island-battles), at commit [`c8b43b3`](https://github.com/INTODAN/island-battles/tree/c8b43b3a519d6bafdc8a3b4c5b1835ea2e2e1348) (paths under `Island Battles Game/Assets/`).

| File | Source | Commit | Licence |
|---|---|---|---|
| `castles/castle-level-1.glb` | `Models/Castle_level_1.fbx`, with the material split of `Models/Castle_all_team.fbx` | `c8b43b3` | the owner's own asset |
| `castles/castle-level-2.glb` | `Models/Castle_level_2.fbx`, with the material split of `Models/Castle_all_team.fbx` | `c8b43b3` | the owner's own asset |
| `castles/castle-level-3.glb` | `Models/Castle_level_3.fbx` (castle, flag, skin and `Take 001` clip, trimmed as in `Models/Castle_level_3.fbx.meta`), with the material split of `Models/Castle_all_team.fbx` | `c8b43b3` | the owner's own asset |
| `castles/castle-level-4.glb` | `Models/Castle_level_4.fbx` (castle, flags, blades, skins and `Take 001` clip, trimmed as in `Models/Castle_level_4.fbx.meta`), with the material split of `Models/Castle_all_team.fbx` | `c8b43b3` | the owner's own asset |
| `castles/atlas.png` | `Textures/Castles/basic_mat_color.png` (pixels unchanged; colour profile and metadata dropped) | `c8b43b3` | the owner's own asset |

The board's other art (#102) is made in this repository. Islands, boats, trees and rocks are built in code (`src/scene/landforms.ts`, `src/scene/art.ts`) on the castle atlas above, and the particle textures are drawn in code (`src/scene/particles.ts`); none of them is a file. No third-party pack is used.

| File | Source | Licence |
|---|---|---|
| `environment/sky.png` | Made by [`tools/environment/sky.ts`](../tools/environment/sky.ts): a 256 by 128 equirectangular sky gradient with the sun's glow, drawn from the sun direction in `src/scene/lookConstants.ts`. Run `bun run tools/environment/sky.ts` to make it again | the owner's own asset, made in this repository |
