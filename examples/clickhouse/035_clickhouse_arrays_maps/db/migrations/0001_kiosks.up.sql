CREATE TABLE kiosks (
    id       UInt64,
    name     String,
    town     LowCardinality(String),
    till     IPv4,
    prices   Map(String, UInt32),
    tags     Array(String),
    location Tuple(lat Float64, lon Float64)
) ENGINE = MergeTree() ORDER BY id;
