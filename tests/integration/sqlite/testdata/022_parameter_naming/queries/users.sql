-- sgl.query(name: FindUserByEmail, command: one)
-- ?1 as sgl.param(email)
SELECT id, name, email FROM users WHERE email = ?;

-- sgl.query(name: InsertUser, command: exec)
-- ?1 as sgl.param(name)
-- ?2 as sgl.param(email)
INSERT INTO users (name, email) VALUES (?, ?);
