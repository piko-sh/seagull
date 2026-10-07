-- sgl.query(name: FindUser, command: one)
-- :name as sgl.param
-- :email as sgl.param
SELECT id, name, email, active FROM users WHERE name = :name AND email = :email;
