CREATE TABLE sailings (
    id          UInt64,
    origin      String,
    destination String,
    departs     String,
    seats_free  Int32
) ENGINE = MergeTree() ORDER BY id;
