-- sgl.query(name: CreateSession, command: exec)
INSERT INTO sessions (id, user_id, device, ip) VALUES (UUID_TO_BIN(?, 1), ?, ?, ?);

-- sgl.query(name: ListSessions, command: many)
SELECT BIN_TO_UUID(id, 1) AS id, user_id, IS_IPV4(ip) AS v4, IS_IPV6(ip) AS v6,
       IS_IPV4_MAPPED(INET6_ATON(ip)) AS mapped, IS_IPV4_COMPAT(INET6_ATON(ip)) AS compat
FROM sessions;

-- sgl.query(name: DistinctPairs, command: one)
SELECT COUNT(DISTINCT user_id, device) AS pairs FROM sessions;

-- sgl.query(name: Advance, command: exec)
UPDATE sequences SET value = LAST_INSERT_ID(value + 1) WHERE name = ?;

-- sgl.query(name: Current, command: one)
SELECT LAST_INSERT_ID() AS value;
