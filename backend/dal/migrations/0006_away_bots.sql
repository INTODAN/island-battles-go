-- Rated games between bots whose owners aren't there (#412). A bot name plays
-- away when it sits in a bot seat, or when the member who chose it doesn't own
-- it. One that misses a turn while it plays away is paused: it plays away in
-- no rated game until its owner resumes it, which clears both columns. Every
-- competitor starts unpaused.

ALTER TABLE island_competitors ADD COLUMN away_paused_at timestamp with time zone;
ALTER TABLE island_competitors ADD COLUMN away_paused_reason text;

-- Whether the competitor played away in the rated game, which its history
-- shows. Every earlier row was played at home.
ALTER TABLE island_rating_events ADD COLUMN away boolean NOT NULL DEFAULT false;
