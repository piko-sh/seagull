CREATE TABLE hauls (
    id         UInt64,
    boat       String,
    species    Array(String),
    weights_kg Array(UInt32)
) ENGINE = MergeTree() ORDER BY id;
