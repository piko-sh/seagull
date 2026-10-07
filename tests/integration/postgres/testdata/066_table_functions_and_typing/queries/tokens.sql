-- sgl.query(name: AddToken, command: exec)
INSERT INTO tokens (id, label) VALUES ($1, $2);

-- sgl.query(name: TokenSizes, command: many)
SELECT label, length(id) AS size, encode(id, 'hex') AS hex FROM tokens ORDER BY label;
