CREATE TABLE lookups (
    id UInt64,
    list_keyed Map(Array(UInt8), String),
    pair_keyed Map(Tuple(x UInt8), String),
    unnamed_keyed Map(Tuple(UInt8, String), String),
    map_keyed Map(Map(String, String), String),
    decimal_keyed Map(Decimal(9, 2), String),
    listed_decimal_keyed Array(Map(Decimal(18, 4), UInt8)),
    held_list_keyed Tuple(a UInt8, m Map(Array(String), UInt8)),
    enum_keyed Map(Enum8('a' = 1, 'b' = 2), String),
    uuid_keyed Map(UUID, String),
    date_keyed Map(Date, String),
    text_keyed Map(LowCardinality(String), String)
) ENGINE = MergeTree ORDER BY id;
