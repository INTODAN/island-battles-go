---
name: testing-and-debugging-workflow
description: Testing and debugging workflows, visual verification via export-to-browser, and database tests.
---

# Testing & Debugging Workflow

### System Tests Requirement
For every task/feature, a **system test** is expected.
*   **Definition:** A system test initializes a test DB, uses the [testutils.CreateGenerationForTest](file:///C:/Users/itaie/ZedProjects/island-battles-go/backend/internal/testutils/test_generation.go#L25) (or `CreateComplexGenerationForTest`) function to create a game, performs operations (such as command execution or generation migrations) on that game, and finally generates a visual graph.
*   **Visual Generation:** At the end of the system test, invoke `testutils.CreateGameGraphForTest(t, db, generation.GameID)` so that the final state is captured.
*   **Importance:** This enables developers to easily verify the logic and flow of the test visually in a browser using the `EXPORT_GAME_GRAPH` workflow.

### Visual Verification (Crucial for Game Logic)
The project uses an "Export-to-Browser" methodology for verifying complex game states.
1.  **Run Tests with Export:**
    `export EXPORT_GAME_GRAPH=true && go test ./backend/services/... -v`
2.  **Inspect Output:** Check the terminal for a path to a temporary HTML file (e.g., `C:\Users\...\tmp\game_graph_*.html`).
3.  **Validate:** Open in a browser to check for overlaps, movement logic, or ECharts rendering errors in the console.

### Database Tests
Uses **Testcontainers** to spin up transient Postgres instances. Ensure Docker is running before executing these tests.
