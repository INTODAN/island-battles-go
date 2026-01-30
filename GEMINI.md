# Gemini Context & Instructions

## Behavioral Guidelines

*Adapted from Andrej Karpathy's specific LLM guidelines.*

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

### 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

### 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:

```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

# Project Specific Instructions - Island Battles Go

## Project Overview
Real-time multiplayer game backend using a language-agnostic architecture.
- **Backend:** Go 1.25, Huma v2 (API), Chi (Router), GORM (ORM).
- **Real-time:** Centrifugo v6 (WebSocket proxy).
- **Database:** PostgreSQL v18.
- **Visualization:** ECharts (via `go-echarts`).

## Core Philosophies & Coding Standards

### 1. Database & GORM
- **No Raw Strings in Queries:** Always use Go structs or constants for query filters.
  - **Avoid:** `.Where("game_id = ?", id)`
  - **Prefer:** `.Where(&models.Game{ID: id})`
- **Avoid `First` Errors:** Prefer `Limit(1).Find(&slice)` over `First(&struct)` to handle "not found" cases gracefully without checking for `gorm.ErrRecordNotFound`.
- **DAL Layer:** All database interactions should reside in `backend/dal/`.

### 2. API Development (Huma v2)
- **Summaries:** When adding or modifying a handler, always update the `Summary` and `Description` in the Huma route registration (`backend/routes/`).
- **Handlers:** Keep handler logic in `backend/handlers/` and keep them thin, delegating complex logic to `backend/services/` or `backend/dal/`.

### 3. Naming & Style
- **Verbose Variables:** Use clear, descriptive variable names. Avoid abbreviations (e.g., use `soldierGroup` instead of `sg`).
- **Idiomatic Go:** Follow standard Go formatting (`go fmt`) and linting (`go vet`).

### 4. Agent Continuity
- **Respect Deletions:** If a user deletes a function or block of code previously added by an agent, **do not** re-add it in subsequent turns unless explicitly requested.

## Project Structure Reference
- `backend/dal/`: Data Access Layer. Contains GORM logic for specific entities.
- `backend/handlers/`: HTTP request handlers (Huma/Chi).
- `backend/models/`: GORM models and shared data structures.
- `backend/services/`: Core game logic (e.g., generation migration, position calculations).
- `backend/dependencies/`: Singleton-style access to DB and Centrifugo clients.
- `client/`: Reference Go CLI client implementation.

## Testing & Debugging Workflow

### Visual Verification (Crucial for Game Logic)
The project uses an "Export-to-Browser" methodology for verifying complex game states.
1.  **Run Tests with Export:**
    `export EXPORT_GAME_GRAPH=true && go test ./backend/services/... -v`
2.  **Inspect Output:** Check the terminal for a path to a temporary HTML file (e.g., `C:\Users\...\tmp\game_graph_*.html`).
3.  **Validate:** Open in a browser to check for overlaps, movement logic, or ECharts rendering errors in the console.

### Database Tests
Uses **Testcontainers** to spin up transient Postgres instances. Ensure Docker is running before executing these tests.

## Common Commands
- **Run Backend (Hot Reload):** `cd backend && air`
- **Run Backend (Standard):** `cd backend && go run main.go`
- **Run Client:** `cd client && go run main.go`
- **Lint:** `go vet ./...`
- **Test:** `go test ./...`
