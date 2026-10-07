CREATE TABLE orders (id UInt64, basket JSON, pos Tuple(name String, value UInt32)) ENGINE = MergeTree ORDER BY id;
