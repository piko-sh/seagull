-- sgl.query(name: SearchUsers, command: many)
-- $1 as sgl.param(name, optional: true)
-- $2 as sgl.param(role, optional: true)
SELECT id, name, email, role FROM users WHERE ($1 IS NULL OR name = $1) AND ($2 IS NULL OR role = $2) ORDER BY id ASC;
