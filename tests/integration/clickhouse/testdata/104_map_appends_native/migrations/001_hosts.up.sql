CREATE TABLE hosts (
    id UInt64,
    keyed Map(String, IPv4),
    keyed_v6 Map(String, IPv6),
    optional Map(String, Nullable(IPv4)),
    listed Map(String, Array(IPv4)),
    listed_optional Map(String, Array(Nullable(IPv6))),
    nested Map(String, Map(String, IPv6)),
    maps Array(Map(String, IPv4)),
    holder Tuple(a UInt8, m Map(String, IPv4)),
    pairs Map(String, Tuple(x UInt8, ip IPv4)),
    listed_pairs Map(String, Array(Tuple(x UInt8, y String))),
    unnamed Map(String, Tuple(UInt8, IPv4)),
    wide Map(String, Int128),
    wide_keyed Map(UInt256, String),
    nullable_wide Map(String, Nullable(Int128)),
    wide_lists Map(String, Array(Int256))
) ENGINE = MergeTree ORDER BY id;
