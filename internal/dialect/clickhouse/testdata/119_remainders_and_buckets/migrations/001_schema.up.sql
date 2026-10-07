CREATE TABLE readings (id UInt32, sensor Int64, slot Int8, small Int16, wide Int32, level Float64, buckets UInt8,
    point Tuple(x Float64, y Float64)) ENGINE = MergeTree ORDER BY id;
