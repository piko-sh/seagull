CREATE TABLE sales (
    id UInt64,
    region String,
    city String,
    amount Decimal(10, 2)
) ENGINE = MergeTree ORDER BY id;
