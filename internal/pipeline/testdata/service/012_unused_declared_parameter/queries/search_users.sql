-- sgl.query(name: SearchUsers, command: many)
-- $1 as sgl.param(userId)
-- $2 as sgl.param(userName)
-- $3 as sgl.param(unused)
SELECT id, name FROM users WHERE id = $1 AND name = $2
