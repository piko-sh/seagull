CREATE TABLE prices (
    id UInt64,
    amount Decimal(10, 2),
    discount Nullable(Decimal(10, 2)),
    label Nullable(String)
) ENGINE = MergeTree ORDER BY id;
