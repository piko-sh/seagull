CREATE TABLE boats (
    id   UInt64,
    name String
) ENGINE = MergeTree() ORDER BY id;
