CREATE TABLE readings (
    id UInt64,
    name String,
    attributes Map(String, UInt64),
    flags Map(Bool, UInt8)
) ENGINE = MergeTree ORDER BY id;
