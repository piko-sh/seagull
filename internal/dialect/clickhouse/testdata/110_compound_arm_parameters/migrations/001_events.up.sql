CREATE TABLE events (
    id UInt64,
    kind String,
    score Int32
) ENGINE = MergeTree ORDER BY id;
