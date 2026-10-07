CREATE FUNCTION Doubled AS x -> x * 2;
CREATE FUNCTION doubled AS x -> toString(x);
CREATE TABLE readings (id UInt64, value UInt32) ENGINE = MergeTree ORDER BY id;
