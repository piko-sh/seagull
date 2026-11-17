CREATE TABLE items (
    id UInt64,
    name String,
    category String,
    price UInt32
) ENGINE = MergeTree() ORDER BY id;
