CREATE TABLE events (
    id UInt64,
    host String,
    note Nullable(String),
    day Date,
    seen_at DateTime64(3, 'UTC'),
    in_stock Bool,
    amount Decimal(18, 4)
) ENGINE = MergeTree ORDER BY id;
