-- sgl.query(name: GetUserByEmail, command: one)
-- :email as sgl.param
SELECT id, name, email FROM users WHERE email = :email;

-- sgl.query(name: InsertUser, command: exec)
-- :user_name as sgl.param
-- :user_email as sgl.param
INSERT INTO users (name, email) VALUES (:user_name, :user_email);
