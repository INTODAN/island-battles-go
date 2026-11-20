-- The rating history: one row for each competitor of each rated game, written
-- in the transaction that stores the game's outcome. It holds what players
-- earned, so it is never reset, and it has no foreign key to the game, whose
-- rows game retention deletes: the history outlives it. The unique pair of
-- game and competitor is what stops a game from being rated twice.

CREATE TABLE island_rating_events (
    id bigserial PRIMARY KEY,
    game_id bigint NOT NULL,
    competitor_id uuid NOT NULL,
    result text NOT NULL,
    rating_before double precision NOT NULL,
    rating_after double precision NOT NULL,
    k integer NOT NULL,
    opponents jsonb NOT NULL,
    ended_at timestamp with time zone NOT NULL,
    CONSTRAINT ck_island_rating_events_result CHECK (result IN ('win', 'draw', 'loss'))
);

CREATE UNIQUE INDEX idx_island_rating_events_game_competitor ON island_rating_events (game_id, competitor_id);
CREATE INDEX idx_island_rating_events_competitor_ended ON island_rating_events (competitor_id, ended_at);
