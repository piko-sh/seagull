-- sgl.query(name: GenerateToken, command: one)
SELECT UUID() AS token_value;

-- sgl.query(name: InsertToken, command: exec)
INSERT INTO tokens (token) VALUES (?);
