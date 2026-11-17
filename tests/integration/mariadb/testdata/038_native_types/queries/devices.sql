-- sgl.query(name: ListDevices, command: many)
SELECT * FROM devices ORDER BY id;

-- sgl.query(name: AddDevice, command: exec)
INSERT INTO devices (id, public_id, owner, last_ip4, last_ip6, embedding) VALUES (?, ?, ?, ?, ?, VEC_FromText(?));

-- sgl.query(name: Addresses, command: many)
SELECT id, last_ip6 AS ip_text, IS_IPV4(last_ip4) AS v4, IS_IPV6(last_ip6) AS v6, VEC_ToText(embedding) AS vector_text
FROM devices
WHERE public_id = ?;
