-- sgl.query(name: HistoricalBalance, command: one)
SELECT id, name, balance FROM accounts AS OF SYSTEM TIME '-10s' WHERE id = $1;

-- sgl.query(name: FollowerOwners, command: many)
SELECT a.id, o.label FROM accounts AS a JOIN owners AS o ON o.id = a.owner_id AS OF SYSTEM TIME follower_read_timestamp() ORDER BY a.id LIMIT $1::INT;

-- sgl.query(name: Renamed, command: many)
SELECT r.key, r.title FROM accounts AS r (key, title) WHERE r.key = $1;

-- sgl.query(name: WithNone, command: many)
VALUES (0, 'none') UNION ALL SELECT id, label FROM owners;

-- sgl.query(name: Lengths, command: many)
SELECT character_length(name) AS characters, date(now()) AS today FROM accounts;

-- sgl.query(name: Missing, command: many)
SELECT date_add(now(), INTERVAL '1 day') AS tomorrow, txid_current() AS transaction_id FROM accounts;
