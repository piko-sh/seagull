-- sgl.query(name: GetUser, command: one)
SELECT id, name, email FROM users WHERE id = ?;

-- sgl.query(name: ListUsers, command: many)
SELECT id, name, email FROM users ORDER BY id;

-- sgl.query(name: CreateUser, command: one)
INSERT INTO users (name, email) VALUES (?, ?) RETURNING id, name, email;
