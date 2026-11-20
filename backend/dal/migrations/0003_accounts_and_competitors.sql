-- The lobby's accounts as the game knows them, and their competitors: the
-- registered bot names, each account's hand play, and the house bots, one per
-- default bot level. They hold what players earned, so they are never reset,
-- and nothing ties them to a game: they outlive every game, and game
-- retention never deletes them.

CREATE TABLE island_accounts (
    account_id uuid PRIMARY KEY,
    display_name text NOT NULL,
    hidden boolean NOT NULL DEFAULT false,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);

CREATE TABLE island_competitors (
    id uuid PRIMARY KEY,
    kind text NOT NULL,
    account_id uuid,
    name text,
    name_key text,
    house_level integer,
    rating double precision NOT NULL DEFAULT 1500,
    games integer NOT NULL DEFAULT 0,
    wins integer NOT NULL DEFAULT 0,
    draws integer NOT NULL DEFAULT 0,
    losses integer NOT NULL DEFAULT 0,
    last_played_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT ck_island_competitors_kind CHECK (kind IN ('bot', 'manual', 'house'))
);

-- Deleting a competitor is soft, so a game in play can still find it. It
-- clears name and name_key, which frees the name at once.
CREATE UNIQUE INDEX idx_island_competitors_name_key ON island_competitors (name_key) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_island_competitors_manual ON island_competitors (account_id) WHERE kind = 'manual' AND deleted_at IS NULL;
CREATE UNIQUE INDEX idx_island_competitors_house_level ON island_competitors (house_level) WHERE kind = 'house' AND deleted_at IS NULL;
CREATE INDEX idx_island_competitors_account ON island_competitors (account_id, created_at) WHERE deleted_at IS NULL;
