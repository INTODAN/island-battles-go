-- pg_dump --schema-only of map_templates and castle_templates as GORM
-- AutoMigrate created them, before the schema was versioned (main at
-- f2ccd85). Tests use it as a database to adopt; never update it to match new
-- migrations.

CREATE TABLE castle_templates (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    team_color bigint,
    "position" point,
    persistent_castle_id bigint,
    upgrades jsonb NOT NULL,
    map_template_id bigint
);

CREATE SEQUENCE castle_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE castle_templates_id_seq OWNED BY castle_templates.id;

CREATE TABLE map_templates (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    map_name text,
    player_amount bigint,
    plane_size_y bigint,
    plane_size_x bigint,
    neutral_starting_soldier_amount bigint
);

CREATE SEQUENCE map_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE map_templates_id_seq OWNED BY map_templates.id;

ALTER TABLE ONLY castle_templates ALTER COLUMN id SET DEFAULT nextval('castle_templates_id_seq'::regclass);

ALTER TABLE ONLY map_templates ALTER COLUMN id SET DEFAULT nextval('map_templates_id_seq'::regclass);

ALTER TABLE ONLY castle_templates
    ADD CONSTRAINT castle_templates_pkey PRIMARY KEY (id);

ALTER TABLE ONLY map_templates
    ADD CONSTRAINT map_templates_pkey PRIMARY KEY (id);

CREATE INDEX idx_castle_templates_deleted_at ON castle_templates USING btree (deleted_at);

CREATE INDEX idx_map_templates_deleted_at ON map_templates USING btree (deleted_at);

ALTER TABLE ONLY castle_templates
    ADD CONSTRAINT fk_map_templates_castle_templates FOREIGN KEY (map_template_id) REFERENCES map_templates(id);
