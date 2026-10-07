-- sgl.query(name: GetUserByID, command: one)
-- @user_id as sgl.param
SELECT id, name, email FROM users WHERE id = @user_id;

-- sgl.query(name: InsertUser, command: exec)
-- @name as sgl.param
-- @email as sgl.param
INSERT INTO users (name, email) VALUES (@name, @email);
