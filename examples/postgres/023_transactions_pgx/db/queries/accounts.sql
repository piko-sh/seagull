-- sgl.query(OpenAccount, one)
INSERT INTO accounts (owner, balance) VALUES ($1, $2) RETURNING id;

-- sgl.query(Deposit, exec)
UPDATE accounts SET balance = balance + :amount WHERE id = :id;

-- sgl.query(Withdraw, exec)
UPDATE accounts SET balance = balance - :amount WHERE id = :id;

-- sgl.query(ListAccounts, many)
SELECT id, owner, balance FROM accounts ORDER BY id;
