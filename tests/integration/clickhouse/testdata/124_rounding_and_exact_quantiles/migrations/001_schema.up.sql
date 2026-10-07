CREATE TABLE orders (
    id UInt64,
    price Decimal(12, 2),
    latency_ms UInt32,
    placed_on Date,
    ratio Float32,
    discount Nullable(Decimal(9, 3))
) ENGINE = MergeTree ORDER BY id;
