-- sgl.query(GetUser, one)
-- sgl.column(does_not_exist, type: text)
SELECT id, email FROM users WHERE id = ?;
