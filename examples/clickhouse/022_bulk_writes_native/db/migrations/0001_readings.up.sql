CREATE TABLE readings (
    station   String,
    minute    UInt16,
    height_cm UInt16,
    note      Nullable(String)
) ENGINE = MergeTree() ORDER BY (station, minute);
