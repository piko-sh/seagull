-- sgl.query(name: GetAccount, command: one)
SELECT id, name, balance FROM accounts WHERE id = ?;

-- sgl.query(name: UpdateBalance, command: exec)
UPDATE accounts SET balance = ? WHERE id = ?;

-- sgl.query(name: ListAccounts, command: many)
SELECT id, name, balance FROM accounts ORDER BY id;
