CREATE TABLE devices (
    id UInt64,
    addr IPv4,
    label String,
    previous Nullable(IPv4)
) ENGINE = MergeTree ORDER BY id;
