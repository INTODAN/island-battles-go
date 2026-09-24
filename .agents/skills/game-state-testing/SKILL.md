---
name: game-state-testing
description: Use Island Battles PostgreSQL fixtures and graph exports when testing generation migrations, game commands, soldier/castle state, or debugging ECharts game visualizations.
---

# Game-state testing

## Database fixtures

- [testutils.InitTestDB(t)](../../../backend/internal/testutils/test_database.go) starts a disposable PostgreSQL container and registers cleanup. Docker must be available; the image may need to be pulled. These tests do not need the Compose database.
- [CreateGenerationForTest(t, db) and CreateComplexGenerationForTest(t, db)](../../../backend/internal/testutils/test_generation.go) return a **generation ID**. Load the generation through `dal.GetGeneration` when you need its state or `GameID`.
- Service tests using these helpers belong to the external `services_test` package: `testutils` itself imports `services`.
- Use database-backed scenarios when the behavior depends on persisted state or generation transitions. Assert the relevant state changes; graph exports are an additional diagnostic for cases that benefit from visualization.

See [existing game graph tests](../../../backend/services/game_graph_test.go) for fixture, command, and migration examples.

## Graph export and inspection

Call `testutils.CreateGameGraphForTest(t, db, generation.GameID)` when a scenario needs an HTML graph. The helper returns HTML and writes a temporary file only when `EXPORT_GAME_GRAPH` is present in the environment. Setting it to `false` still enables export; unset it to disable export.

To export an existing scenario, run from the repository root:

```sh
EXPORT_GAME_GRAPH=true go test ./backend/services -run '^TestGameGraphWithSoldierGroups$' -count=1 -v
```

The command scopes the environment variable to this run and bypasses cached test results so the HTML is regenerated. Use the specific test relevant to the change.

Find `created temporary file:` in the output. For visualization changes, open that HTML in a browser and inspect movement, castle values, layout, and browser-console errors as relevant. If browser inspection is unavailable, report that limitation separately from the Go test result.
