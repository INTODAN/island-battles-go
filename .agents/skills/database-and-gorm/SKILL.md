---
name: database-and-gorm
description: GORM database rules, query filters, DAL layer requirements, and avoiding First errors.
---

# Database & GORM

- **No Raw Strings in Queries:** Always use Go structs or constants for query filters.
  - **Avoid:** `.Where("game_id = ?", id)`
  - **Prefer:** `.Where(&models.Game{ID: id})`
- **Avoid `First` Errors:** Prefer `Limit(1).Find(&slice)` over `First(&struct)` to handle "not found" cases gracefully without checking for `gorm.ErrRecordNotFound`.
- **DAL Layer:** All database interactions should reside in `backend/dal/`.
