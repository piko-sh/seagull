-- sgl.query(OpenAccount, one)
INSERT INTO accounts (owner, balance) VALUES ($1, $2) RETURNING id;

-- sgl.query(Deposit, exec)
-- $1 as sgl.param(amount)
UPDATE accounts SET balance = balance + $1 WHERE id = $2;

-- sgl.query(Withdraw, exec)
-- $1 as sgl.param(amount)
UPDATE accounts SET balance = balance - $1 WHERE id = $2;

-- sgl.query(GetBalance, one)
SELECT balance FROM accounts WHERE id = $1;

-- sgl.query(SetBalance, exec)
UPDATE accounts SET balance = $1 WHERE id = $2;

-- sgl.query(ListAccounts, many)
SELECT id, owner, balance FROM accounts ORDER BY id;
