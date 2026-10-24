-- A castle's own starting soldier count from the map file format. NULL starts
-- the castle with its template's neutral_starting_soldier_amount, so every
-- existing row keeps the count it had. IF NOT EXISTS keeps databases that
-- GORM AutoMigrate already gave the column before this migration existed.

ALTER TABLE castle_templates ADD COLUMN IF NOT EXISTS starting_soldier_amount bigint;
