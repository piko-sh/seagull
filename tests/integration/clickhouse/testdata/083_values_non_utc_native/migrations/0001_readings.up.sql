CREATE TABLE readings (
    id UInt64,
    day Date,
    day32 Date32,
    taken_at DateTime,
    taken_at_ms DateTime64(3),
    sensors Array(UUID),
    gateways Array(IPv4),
    frames Array(Array(UInt8)),
    totals Array(Int128),
    note String,
    position Tuple(x Float64, y Float64),
    tags Map(String, UInt32)
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE notes (
    id UInt64,
    body String
) ENGINE = MergeTree ORDER BY id;
