CREATE TABLE gulls (
    id UInt64,
    name String,
    note Nullable(String),
    score UInt32,
    active Bool,
    tags Array(String),
    ringings Map(String, UInt32)
) ENGINE = MergeTree ORDER BY id;
