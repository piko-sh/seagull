CREATE TABLE tasks (
    id         UInt64,
    title      String,
    completed  UInt8    DEFAULT 0,
    created_at DateTime DEFAULT now()
) ENGINE = MergeTree() ORDER BY id;
