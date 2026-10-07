CREATE TABLE boats (
    id     UInt64,
    name   String,
    status Enum8('moored' = 1, 'fishing' = 2, 'in_dock' = 3)
) ENGINE = MergeTree() ORDER BY id;
