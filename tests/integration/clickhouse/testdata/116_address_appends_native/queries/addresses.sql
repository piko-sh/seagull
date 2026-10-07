-- sgl.query(CopyAddresses, copyfrom)
INSERT INTO addresses (id, host, peer, gateway, relay, hops, routes, link, origin, aliases, fallbacks, mirrors)
VALUES ({id:UInt64}, {host:IPv4}, {peer:IPv6}, {gateway:Nullable(IPv4)}, {relay:Nullable(IPv6)}, {hops:Array(IPv4)},
    {routes:Array(Nullable(IPv4))}, {link:Tuple(near IPv4, far IPv6)}, {origin:Tuple(IPv4, UInt8)}, {aliases:Map(String, IPv4)},
    {fallbacks:Map(String, Nullable(IPv4))}, {mirrors:Map(String, IPv6)});

-- sgl.query(InsertAddresses, batch)
INSERT INTO addresses (id, host, peer, gateway, relay, hops, routes, link, origin, aliases, fallbacks, mirrors)
VALUES ({id:UInt64}, {host:IPv4}, {peer:IPv6}, {gateway:Nullable(IPv4)}, {relay:Nullable(IPv6)}, {hops:Array(IPv4)},
    {routes:Array(Nullable(IPv4))}, {link:Tuple(near IPv4, far IPv6)}, {origin:Tuple(IPv4, UInt8)}, {aliases:Map(String, IPv4)},
    {fallbacks:Map(String, Nullable(IPv4))}, {mirrors:Map(String, IPv6)});

-- sgl.query(InsertAddressRows, batch)
INSERT INTO addresses (id, host, peer, gateway, relay, hops, routes, link, origin, aliases, fallbacks, mirrors)
VALUES ({id:UInt64} + 0, {host:IPv4}, {peer:IPv6}, {gateway:Nullable(IPv4)}, {relay:Nullable(IPv6)}, {hops:Array(IPv4)},
    {routes:Array(Nullable(IPv4))}, {link:Tuple(near IPv4, far IPv6)}, {origin:Tuple(IPv4, UInt8)}, {aliases:Map(String, IPv4)},
    {fallbacks:Map(String, Nullable(IPv4))}, {mirrors:Map(String, IPv6)});

-- sgl.query(RecordAddress, exec)
INSERT INTO addresses (id, host, peer, gateway, relay, hops, routes, link, origin, aliases, fallbacks, mirrors)
VALUES ({id:UInt64}, {host:IPv4}, {peer:IPv6}, {gateway:Nullable(IPv4)}, {relay:Nullable(IPv6)}, {hops:Array(IPv4)},
    {routes:Array(Nullable(IPv4))}, {link:Tuple(near IPv4, far IPv6)}, {origin:Tuple(IPv4, UInt8)}, {aliases:Map(String, IPv4)},
    {fallbacks:Map(String, Nullable(IPv4))}, {mirrors:Map(String, IPv6)});

-- sgl.query(ListAddresses, many)
SELECT id, host, peer, gateway, relay, hops, routes, link, toString(origin) AS origin, aliases, fallbacks, mirrors
FROM addresses ORDER BY id;
