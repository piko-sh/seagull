CREATE TABLE kiosks (
    id UInt64,
    location Tuple(lat Float64, lon Float64),
    stock Map(String, UInt32),
    shelves Array(Map(String, UInt32))
) ENGINE = MergeTree ORDER BY id;
