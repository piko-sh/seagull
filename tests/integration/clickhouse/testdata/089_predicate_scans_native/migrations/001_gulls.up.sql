CREATE TABLE gulls (
    id UInt64,
    wing Int32,
    span Nullable(Int32),
    ringed Bool,
    tagged Nullable(Bool)
) ENGINE = MergeTree ORDER BY id;
