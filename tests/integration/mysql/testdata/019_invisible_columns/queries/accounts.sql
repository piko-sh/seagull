-- sgl.query(name: ListAccounts, command: many)
SELECT * FROM accounts ORDER BY id;

-- sgl.query(name: ListQualifiedAccounts, command: many)
SELECT a.* FROM accounts AS a ORDER BY a.id;

-- sgl.query(name: GetToken, command: one)
SELECT id, token FROM accounts WHERE id = ?;
