-- Test-owned pre-cutover connection tables. Full simulation graph preservation
-- is also covered independently in backend/migrations/legacy_lobby_test.go.
CREATE TABLE games (
    id bigserial PRIMARY KEY,
    created_at timestamptz, updated_at timestamptz, deleted_at timestamptz,
    room_name text UNIQUE NOT NULL DEFAULT 'test-game'
);
CREATE TABLE players (
    id bigint PRIMARY KEY, game_id bigint REFERENCES games(id), team_color integer,
    created_at timestamptz, updated_at timestamptz, deleted_at timestamptz
);
INSERT INTO games (room_name,created_at,updated_at,deleted_at) VALUES
    ('legacy-room','2020-01-01Z','2020-02-01Z',NULL),
    ('deleted-room','2020-01-01Z','2020-02-01Z','2020-03-01Z');
INSERT INTO players VALUES
    (9007199254740993,1,-7,'2020-01-01Z','2020-02-01Z',NULL),
    (2,1,-7,'2020-01-01Z','2020-02-01Z','2020-03-01Z');
