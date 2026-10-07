CREATE TABLE items (
    id UInt64,
    mood Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3),
    maybe Nullable(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)),
    moods Array(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3)),
    maybes Array(Nullable(Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))),
    labels Map(String, Enum8('calm' = 1, 'stormy' = 2, 'fair' = 3))
) ENGINE = MergeTree ORDER BY id;
