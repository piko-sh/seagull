CREATE FUNCTION times_two AS x -> x * 2;
CREATE FUNCTION add_xy AS (x, y) -> x + y;
CREATE TABLE readings (id UInt64, value UInt32) ENGINE = MergeTree ORDER BY id;
