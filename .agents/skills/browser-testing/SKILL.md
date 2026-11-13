---
name: browser-testing
description: Use when testing the lobby UI, the demo game or the replay view in a real browser through the Playwright MCP tools. Covers launching a debuggable Chrome, running the app, and avoiding the timeouts that make clicks and screenshots fail.
---

# Browser testing with Playwright

The Playwright MCP tools in this project do not launch a browser. They attach over the Chrome DevTools Protocol to `127.0.0.1:9222`, and every call fails with `connect ECONNREFUSED 127.0.0.1:9222` until a browser listens there. Both configured servers (`playwright` and `mcp-server-playwright`) attach to the same port; use one of them consistently.

## Start the app and a dedicated Chrome

Run both as background commands with a long timeout, so they outlive the call that started them.

1. Start the lobby, the backend and their dependencies with `./scripts/dev.sh`, then wait until `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8888/healthz` and the same for the lobby's `http://127.0.0.1:8890/healthz` print `204`. The lobby and the backend are separate processes on separate origins, as in production: open `http://localhost:8890/lobby/ui/` for the lobby page, which loads Island Battles' screens from the backend on `:8888`. For the 3D client, run `bun install` and `bun run dev` in `clients/island-battles-web/` (another background command) and open `http://localhost:3000/`; it calls the lobby on `:8890`. The backend's own pages (`/game/demo-viewer/`, `/map/builder/`) are on `:8888`, and through the dev server on `:3000`. A CORS error in the console means a missing origin in `LOBBY_ALLOWED_ORIGINS` or `ISLAND_BATTLES_LOBBY_ORIGINS`.
2. Launch a separate Chrome with a throwaway profile. Never attach to the user's own profile, and never reuse it.

   ```sh
   google-chrome --remote-debugging-port=9222 \
     --user-data-dir=/tmp/island-battles-playwright-profile \
     --no-first-run --no-default-browser-check --disable-popup-blocking \
     --disable-backgrounding-occluded-windows --disable-renderer-backgrounding \
     --disable-background-timer-throttling about:blank
   ```

   The last three flags keep the window rendering when it is behind other windows. Without them, screenshots time out. Wait until `curl -s http://127.0.0.1:9222/json/version` answers. If you restart Chrome, the tools reconnect on their next call.
3. Load every tool you need in one `ToolSearch` call, for example `select:mcp__playwright__browser_navigate,mcp__playwright__browser_snapshot,mcp__playwright__browser_click,mcp__playwright__browser_tabs,mcp__playwright__browser_evaluate,mcp__playwright__browser_take_screenshot,mcp__playwright__browser_wait_for,mcp__playwright__browser_console_messages`.

## Avoid failed calls

Clicks and element screenshots wait until the element is visible and stable over consecutive rendered frames. They time out with `waiting for element to be visible, enabled and stable` in two situations:

- **The tab is not the one Chrome shows.** Only the front tab of a window renders. **New demo lobby** opens Sam's tab in front of Alex's. Call `browser_tabs` with `action: "select"` on the tab you are about to use, before clicking or taking screenshots. To confirm, evaluate this snippet; a hidden tab reports `hidden` and 0 frames:

  ```js
  () => new Promise((resolve) => { let frames = 0; const start = performance.now();
    const tick = () => { frames++; if (performance.now() - start < 1000) requestAnimationFrame(tick); };
    requestAnimationFrame(tick);
    setTimeout(() => resolve({ frames, visibility: document.visibilityState }), 1200); })
  ```

- **The layout moves.** A running game re-renders about once per generation and shifts the page. Pause a demo player before clicking or capturing anything: evaluate `() => document.getElementById('example-toggle').click()` on the page; the button reads **Resume this player** once paused.

After a timeout, check these two causes instead of retrying the same call; stop after two attempts.

## Work with the game frame

- The replay is a sandboxed `srcdoc` iframe. Page-level `browser_evaluate` cannot read its `contentDocument`.
- Snapshots include the frame's content with `f1e…` refs. Role selectors such as `internal:role=button[name="Replay"]` do not reach into the frame; click frame elements by their refs.
- To read or click inside the frame from script, pass a frame element's ref as `target` to `browser_evaluate`; the function then runs in the frame, for example `(element) => element.ownerDocument.getElementById('turn').textContent`. Calling `element.click()` this way also avoids the stability wait.
- Refs change when the page re-renders. Take a new snapshot instead of reusing old refs, and scope it with `target` (for example `internal:role=region[name="Game result"]`) to keep it short.

## Files and output

The tools only write under the repository. Save screenshots to `.playwright-mcp/` (ignored by git); paths such as `/tmp` are rejected. Snapshots and console logs are written there too. Filter console output with `level: "warning"`; a 404 for `/favicon.ico` is expected.

For a static page that needs no interaction, a headless check is faster: run Playwright's `chrome-headless-shell` (under `~/.cache/ms-playwright/`) with `--virtual-time-budget=3000 --dump-dom file:///path/page.html` and inspect the printed DOM.

## Clean up

Pause demo players before you finish; a running demo writes a generation about every second. Stop the background Chrome and backend tasks you started, and delete `.playwright-mcp/`. Leave the dev database and Centrifugo containers running, as `scripts/dev.sh` does.
