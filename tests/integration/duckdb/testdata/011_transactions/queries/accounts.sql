-- sgl.query(name: GetAccount, command: one)
SELECT id, name, balance FROM accounts WHERE id = $1;

-- sgl.query(name: UpdateBalance, command: exec)
UPDATE accounts SET balance = $1 WHERE id = $2;

-- sgl.query(name: ListAccounts, command: many)
SELECT id, name, balance FROM accounts ORDER BY id;
