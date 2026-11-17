CREATE TABLE stock (
    id UInt64,
    keyed Map(String, Tuple(a UInt8, b String)),
    listed_keyed Array(Map(String, Tuple(a UInt8, b String))),
    keyed_lists Map(String, Array(Tuple(a UInt8, b String))),
    keyed_keyed Map(String, Map(UInt8, Tuple(a UInt8, b String))),
    dated Map(Date, Tuple(a UInt8)),
    listed_holders Array(Tuple(a UInt8, m Map(String, Tuple(x UInt8)))),
    holder Tuple(a UInt8, m Map(String, Tuple(x UInt8))),
    rich Map(String, Tuple(
        ns Nullable(String), an Array(Nullable(UInt8)), dt DateTime, dec Decimal(10, 2), nd Nullable(Decimal(10, 2)),
        u UUID, lc LowCardinality(String), mp Map(String, UInt8), fs FixedString(2), big Int128, ip4 IPv4, ip6 IPv6,
        b Bool, f Float64, d64 DateTime64(3), nt Tuple(p UInt8, q Nullable(String)))),
    keyed_pairs Map(String, Tuple(UInt8, String)),
    fields Tuple(a UInt8, b String)
) ENGINE = MergeTree ORDER BY id;
