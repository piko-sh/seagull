-- sgl.query(AddAccount, exec)
INSERT INTO accounts (id, owner_id, name) VALUES (?, ?, ?);

-- sgl.query(GetAccount, one)
SELECT id, owner_id, name FROM accounts WHERE id = ?;
