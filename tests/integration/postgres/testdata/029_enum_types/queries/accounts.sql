-- sgl.query(name: InsertAccount, command: one)
INSERT INTO accounts (username, status)
VALUES ($1, $2::status_type)
RETURNING id, username, status;

-- sgl.query(name: ListByStatus, command: many)
SELECT id, username, status
FROM accounts
WHERE status = $1::status_type
ORDER BY id;

-- sgl.query(name: ListAllAccounts, command: many)
SELECT id, username, status
FROM accounts
ORDER BY id;
