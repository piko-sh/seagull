-- sgl.migration(readonly: true)
CREATE FUNCTION times_two_native AS x -> x * 2;
CREATE FUNCTION add_up_native AS (x, y) -> x + y;
CREATE TABLE readings (id UInt64, value UInt32) ENGINE = MergeTree ORDER BY id;
INSERT INTO readings VALUES (1, 21);
