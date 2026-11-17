-- sgl.query(name: GetSessionDetails, command: one)
-- $1 as sgl.param(session_id)
SELECT
    session_id,
    session_account_id,
    version_status,
    version_two_factor_secret
FROM sessions_with_latest_version
WHERE session_id = $1;
