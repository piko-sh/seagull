CREATE TABLE accounts (
    id uuid PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE account_versions (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL,
    status TEXT NOT NULL,
    email TEXT NOT NULL
);

CREATE VIEW accounts_with_latest AS
WITH latest AS (
    SELECT DISTINCT ON (account_id) account_id, id
    FROM account_versions
    ORDER BY account_id, id DESC
)
SELECT
    u.id              AS account_id,
    u.created_at      AS account_created_at,
    v.id              AS version_id,
    v.status          AS version_status,
    v.email           AS version_email
FROM accounts u
    JOIN latest l ON l.account_id = u.id
    JOIN account_versions v ON v.account_id = u.id AND v.id = l.id;
