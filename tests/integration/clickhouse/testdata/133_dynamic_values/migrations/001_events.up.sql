CREATE TABLE events (
    id UInt64,
    payload Dynamic,
    data JSON,
    extras Array(Dynamic)
) ENGINE = MergeTree ORDER BY id;
