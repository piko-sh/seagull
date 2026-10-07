CREATE TABLE IF NOT EXISTS items (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE IF NOT EXISTS items (id bigint PRIMARY KEY, label text);
ALTER TABLE items ADD COLUMN IF NOT EXISTS name text, ADD COLUMN IF NOT EXISTS price integer;
ALTER TABLE IF EXISTS gone ADD COLUMN extra integer;

CREATE TYPE mood AS ENUM ('happy', 'sad');
ALTER TYPE mood ADD VALUE IF NOT EXISTS 'sad';
ALTER TYPE mood ADD VALUE 'calm' BEFORE 'sad';
ALTER TYPE mood RENAME TO feeling;
CREATE TABLE diary (id integer PRIMARY KEY, mood feeling NOT NULL);

CREATE TABLE pairs (left_id integer, right_id integer, note text, PRIMARY KEY (left_id, right_id));
CREATE TABLE later_pairs (left_id integer, right_id integer, note text);
ALTER TABLE later_pairs ADD PRIMARY KEY (left_id, right_id);

CREATE TABLE parent (id bigint NOT NULL, version integer NOT NULL);
CREATE TABLE child (extra text, id bigint NOT NULL) INHERITS (parent);
ALTER TABLE parent ADD COLUMN label text;
CREATE TABLE copied (code text, LIKE items, note text);

CREATE VIEW star_view AS SELECT * FROM items;
CREATE VIEW named_view (item_id, item_name) AS SELECT items.* FROM items;
CREATE MATERIALIZED VIEW items_snapshot AS SELECT * FROM items WITH NO DATA;
ALTER MATERIALIZED VIEW items_snapshot RENAME TO snapshot;
