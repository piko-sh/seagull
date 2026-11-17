CREATE TABLE readings (
    id UInt64,
    keyed_wide Map(String, Int128),
    wide_keyed Map(UInt256, String),
    listed_keyed_wide Array(Map(String, Int128)),
    held_keyed_wide Tuple(a UInt8, m Map(String, Int256)),
    address_keyed Map(IPv4, String),
    wide_pair Tuple(UInt8, Int128),
    listed_wide_pairs Array(Tuple(UInt8, UInt128)),
    keyed_wide_pairs Map(String, Tuple(UInt8, Int128)),
    keyed_nullable_wide Map(String, Nullable(Int128)),
    keyed_wide_lists Map(String, Array(Int128)),
    keyed_wide_fields Map(String, Tuple(a UInt8, b Int128)),
    keyed_addresses Map(String, IPv4),
    nullable_wide_pair Tuple(UInt8, Nullable(Int128)),
    address_pair Tuple(UInt8, IPv6)
) ENGINE = MergeTree ORDER BY id;
