CREATE TABLE trips (
    id           UInt64,
    name         String,
    destination  String,
    price_pence  UInt32,
    dogs_welcome Bool
) ENGINE = MergeTree() ORDER BY id;
