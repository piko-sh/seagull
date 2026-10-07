CREATE TABLE entries (
    id             UInt64,
    boat           String,
    crew           UInt8,
    finish_seconds UInt32
) ENGINE = MergeTree() ORDER BY id;
