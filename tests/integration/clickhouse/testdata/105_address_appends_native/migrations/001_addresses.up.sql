CREATE TABLE addresses (
    id UInt64,
    host IPv4,
    peer IPv6,
    gateway Nullable(IPv4),
    relay Nullable(IPv6),
    hops Array(IPv4),
    routes Array(Nullable(IPv4)),
    link Tuple(near IPv4, far IPv6),
    origin Tuple(IPv4, UInt8),
    aliases Map(String, IPv4),
    fallbacks Map(String, Nullable(IPv4)),
    mirrors Map(String, IPv6)
) ENGINE = MergeTree ORDER BY id;
