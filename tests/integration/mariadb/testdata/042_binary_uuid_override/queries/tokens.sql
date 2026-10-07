-- sgl.query(AddToken, exec)
INSERT INTO tokens (id, label) VALUES (?, ?);

-- sgl.query(GetToken, one)
SELECT id, label FROM tokens WHERE id = ?;

-- sgl.query(TokenBytes, one)
SELECT hex(id) AS hex_id FROM tokens WHERE label = ?;
