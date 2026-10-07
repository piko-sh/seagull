CREATE TABLE menu (
    id          UInt64,
    dish        String,
    price_pence UInt32
) ENGINE = MergeTree() ORDER BY id;
