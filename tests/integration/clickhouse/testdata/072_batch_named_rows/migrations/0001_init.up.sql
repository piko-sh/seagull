CREATE TABLE events (
    id UInt64,
    host String,
    note Nullable(String),
    day Date
) ENGINE = MergeTree ORDER BY id;
