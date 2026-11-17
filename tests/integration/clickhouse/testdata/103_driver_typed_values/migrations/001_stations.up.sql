CREATE TABLE stations (
    id UInt64,
    address IPv4,
    optional Nullable(IPv4),
    wide Int128,
    addressed Tuple(
        ip IPv4, ip6 IPv6, nip Nullable(IPv4), big Int128, nbig Nullable(Int128), huge UInt256,
        ips Array(IPv4), mip Map(String, IPv4), inner Tuple(x IPv4, y Int256), items Array(Tuple(b UInt8, ip IPv6))),
    listed Array(Tuple(a UInt8, ip IPv4, big Int128)),
    addresses Array(IPv4),
    nullable_addresses Array(Nullable(IPv4)),
    keyed_addresses Map(String, IPv6),
    nested_addresses Array(Array(IPv4)),
    holder Tuple(a UInt8, m Map(String, Tuple(x UInt8)))
) ENGINE = MergeTree ORDER BY id;
