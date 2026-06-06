---
name: api-development
description: Huma v2 API development standards, route summaries/descriptions, and thin handler design.
---

# API Development (Huma v2)

- **Summaries:** When adding or modifying a handler, always update the `Summary` and `Description` in the Huma route registration (`backend/routes/`).
- **Handlers:** Keep handler logic in `backend/handlers/` and keep them thin, delegating complex logic to `backend/services/` or `backend/dal/`.
