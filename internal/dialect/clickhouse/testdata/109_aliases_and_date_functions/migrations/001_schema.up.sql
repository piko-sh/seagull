CREATE TABLE events (
    id UInt64,
    user_id UInt32,
    kind String,
    path String,
    bytes Int64,
    flags UInt32,
    amount Decimal(18, 4),
    duration Float64,
    at DateTime,
    at_precise DateTime64(3),
    day Date
) ENGINE = MergeTree ORDER BY (day, id);
