CREATE TABLE orders (
    id       UInt64,
    customer String,
    basket   String,
    delivery Nullable(String)
) ENGINE = MergeTree() ORDER BY id;
