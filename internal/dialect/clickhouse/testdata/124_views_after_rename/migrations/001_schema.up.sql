CREATE TABLE src (id Int64, label String) ENGINE = MergeTree ORDER BY id;
CREATE VIEW v AS SELECT id, label FROM src;
RENAME TABLE src TO dst;
