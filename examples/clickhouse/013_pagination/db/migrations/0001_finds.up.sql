CREATE TABLE finds (
    id       UInt64,
    creature String,
    pool     String,
    size_mm  UInt32
) ENGINE = MergeTree() ORDER BY id;
