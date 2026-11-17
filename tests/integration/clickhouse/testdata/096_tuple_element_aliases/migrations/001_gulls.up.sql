CREATE TABLE gulls (
    id UInt64,
    name String,
    wingspan Nullable(Float64)
) ENGINE = MergeTree ORDER BY id;
