CREATE TABLE trips (
    id UInt64,
    started Date,
    departed DateTime
) ENGINE = MergeTree ORDER BY id;
