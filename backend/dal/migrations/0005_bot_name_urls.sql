-- A bot name's hosted bot URL (#400): where the name plays in a rated lobby,
-- which its owner sets when registering the name and can change, and which
-- the leaderboard shows once the name is listed. The rating stays with the
-- name whatever its URL. Names registered before this migration have none
-- until their owner adds one, and the other kinds of competitor never have
-- one. Deleting a name clears it with the name.

ALTER TABLE island_competitors ADD COLUMN url text;
