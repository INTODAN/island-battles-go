-- Map templates as GORM AutoMigrate created them before migrations were
-- versioned. Databases created by that code are adopted at this version
-- without running it.

CREATE TABLE map_templates (
    id bigserial PRIMARY KEY,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    map_name text,
    player_amount bigint,
    plane_size_y bigint,
    plane_size_x bigint,
    neutral_starting_soldier_amount bigint
);

CREATE INDEX idx_map_templates_deleted_at ON map_templates (deleted_at);

CREATE TABLE castle_templates (
    id bigserial PRIMARY KEY,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    team_color bigint,
    "position" point,
    persistent_castle_id bigint,
    upgrades jsonb NOT NULL,
    map_template_id bigint,
    CONSTRAINT fk_map_templates_castle_templates FOREIGN KEY (map_template_id) REFERENCES map_templates (id)
);

CREATE INDEX idx_castle_templates_deleted_at ON castle_templates (deleted_at);
