CREATE TABLE events (
    id UInt64,
    user_id UInt32,
    kind String,
    path String,
    bytes Int64,
    flags UInt32,
    duration Float64,
    at DateTime('UTC'),
    at_precise DateTime64(3, 'UTC'),
    day Date
) ENGINE = MergeTree ORDER BY (day, id);
