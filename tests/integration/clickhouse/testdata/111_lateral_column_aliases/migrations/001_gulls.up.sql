CREATE TABLE gulls (
    id UInt64,
    name String,
    wingspan Nullable(Float64),
    weight Decimal(6, 2)
) ENGINE = MergeTree ORDER BY id;
