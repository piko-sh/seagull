CREATE TABLE stock (
    id UInt64,
    counts Map(String, UInt32),
    shelves Array(Map(String, UInt8)),
    tagged Tuple(Map(String, UInt8), UInt8),
    nested Map(String, Map(String, UInt8))
) ENGINE = MergeTree ORDER BY id;
