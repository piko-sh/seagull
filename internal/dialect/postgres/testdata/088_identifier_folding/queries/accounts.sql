-- sgl.query(GetAccount, one)
SELECT id, "DisplayName", EMAIL FROM ACCOUNTS WHERE ID = $1;

-- sgl.query(UnquotedDisplayName, many)
SELECT DisplayName FROM accounts;
