CREATE SCHEMA archive;

CREATE TABLE IF NOT EXISTS items (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE IF NOT EXISTS items (id bigint PRIMARY KEY, label text);
ALTER TABLE items ADD COLUMN IF NOT EXISTS name text, ADD COLUMN IF NOT EXISTS price integer;
ALTER TABLE IF EXISTS gone ADD COLUMN extra integer;
ALTER TABLE IF EXISTS gone RENAME TO deprecated_gone;

CREATE TYPE event AS ENUM ('START', 'STOP');
ALTER TYPE event ADD VALUE IF NOT EXISTS 'START';
ALTER TYPE event ADD VALUE 'PAUSE' AFTER 'START';
ALTER TYPE event ADD VALUE 'WAIT' BEFORE 'START';
CREATE TABLE logs (id integer PRIMARY KEY, kind event NOT NULL);
ALTER TYPE event RENAME TO log_event;
ALTER TYPE log_event SET SCHEMA archive;

CREATE TABLE pairs (left_id integer, right_id integer, note text, PRIMARY KEY (left_id, right_id));
CREATE TABLE later_pairs (left_id integer, right_id integer, note text);
ALTER TABLE later_pairs ADD CONSTRAINT later_pairs_key PRIMARY KEY (left_id, right_id);

CREATE TABLE parent (id bigint NOT NULL, created_at timestamptz NOT NULL);
CREATE TABLE child (extra text, id bigint NOT NULL) INHERITS (parent);
ALTER TABLE parent ADD COLUMN updated_at timestamptz;
CREATE TABLE copied (code text, LIKE items INCLUDING ALL, note text);

CREATE VIEW star_view AS SELECT * FROM items;
CREATE VIEW qualified_view AS SELECT items.* FROM items;
CREATE VIEW named_view (item_id, item_name) AS SELECT * FROM items;
CREATE MATERIALIZED VIEW items_snapshot AS SELECT id, name FROM items;
CREATE MATERIALIZED VIEW IF NOT EXISTS items_snapshot AS SELECT id FROM items;
ALTER MATERIALIZED VIEW items_snapshot RENAME TO snapshot;
ALTER MATERIALIZED VIEW snapshot SET SCHEMA archive;
ALTER VIEW star_view RENAME COLUMN name TO item_name;
