CREATE TABLE readings (
    id UInt32,
    delta Int32,
    count UInt32,
    total UInt64,
    offset_value Int64,
    small Int8
) ENGINE = MergeTree ORDER BY id;
INSERT INTO readings VALUES (1, -5, 7, 18446744073709551615, -9, -1), (2, 3, 4000000000, 5, 9, 2);
