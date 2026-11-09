import type { AssetProgress } from "../scene/boardAssets";

// The loading screen over a board while its castles and textures download (issue #102): a line
// of text and a progress bar. It covers the board, which draws its placeholders underneath, and
// fades away once everything is in.

export interface BoardLoading {
  update(progress: AssetProgress): void;
  /** The files are in: fade out and remove. */
  done(): void;
  /** Loading failed: say so briefly and get out of the way, since the board still works. */
  failed(): void;
}

export function showBoardLoading(parent: HTMLElement): BoardLoading {
  const root = document.createElement("div");
  root.className = "board-loading";
  root.setAttribute("role", "status");
  const text = document.createElement("p");
  text.textContent = "Loading the board…";
  const bar = document.createElement("progress");
  bar.max = 100;
  bar.value = 0;
  bar.setAttribute("aria-label", "Loading the board");
  const percent = document.createElement("span");
  percent.className = "board-loading-percent";
  percent.textContent = "0%";
  root.append(text, bar, percent);
  parent.append(root);
  let finished = false;
  const remove = (after: number) => {
    finished = true;
    setTimeout(() => {
      root.classList.add("board-loading-out");
      setTimeout(() => root.remove(), 400);
    }, after);
  };
  return {
    update(progress) {
      if (finished) return;
      const value = Math.round(
        Math.min(1, Math.max(0, progress.fraction)) * 100,
      );
      bar.value = value;
      percent.textContent = `${value}%`;
    },
    done() {
      if (finished) return;
      bar.value = 100;
      percent.textContent = "100%";
      remove(0);
    },
    failed() {
      if (finished) return;
      text.textContent =
        "Some of the board's art could not load; showing simple shapes.";
      remove(2000);
    },
  };
}
