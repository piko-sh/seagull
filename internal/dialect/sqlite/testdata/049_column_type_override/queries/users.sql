-- sgl.query(GetUserEmailLower, one)
-- sgl.column(email_lower, type: text)
SELECT id, LOWER(email) AS email_lower FROM users WHERE id = ?;
