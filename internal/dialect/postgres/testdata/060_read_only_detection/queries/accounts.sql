-- sgl.query(name: GetAccount, command: one)
SELECT id, name, balance FROM accounts WHERE id = $1;

-- sgl.query(name: LockAccount, command: one)
SELECT id, name, balance FROM accounts WHERE id = $1 FOR UPDATE;

-- sgl.query(name: CreateAccount, command: one)
INSERT INTO accounts (name, balance) VALUES ($1, $2) RETURNING id;

-- sgl.query(name: ArchiveAccount, command: exec)
WITH deleted AS (
    DELETE FROM accounts WHERE id = $1 RETURNING id
)
SELECT id FROM deleted;
