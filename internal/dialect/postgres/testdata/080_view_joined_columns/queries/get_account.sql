-- sgl.query(name: GetAccountWithLatestVersion, command: one)
-- $1 as sgl.param(account_id)
SELECT
    account_id,
    account_created_at,
    version_id,
    version_status,
    version_email
FROM accounts_with_latest
WHERE account_id = $1;
