-- sgl.query(name: AddOwner, command: exec)
INSERT INTO owners (id, label) VALUES ($1, $2);

-- sgl.query(name: AddAccount, command: exec)
INSERT INTO accounts (id, name, balance, owner_id) VALUES ($1, $2, $3, $4);

-- sgl.query(name: HistoricalBalance, command: one)
SELECT id, name, balance FROM accounts AS OF SYSTEM TIME '-1s' WHERE id = $1;

-- sgl.query(name: FollowerOwners, command: many)
SELECT a.id, o.label FROM accounts AS a JOIN owners AS o ON o.id = a.owner_id AS OF SYSTEM TIME follower_read_timestamp()
ORDER BY a.id LIMIT $1::INT OFFSET ($2::INT - 1) * $1::INT;

-- sgl.query(name: Renamed, command: many)
SELECT r.key, r.title FROM accounts AS r (key, title) WHERE r.key = $1;

-- sgl.query(name: WithNone, command: many)
VALUES (0, 'none') UNION ALL SELECT id, label FROM owners ORDER BY 1;
