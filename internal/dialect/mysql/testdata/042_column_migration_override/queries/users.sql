-- sgl.query(GetUser, one)
SELECT id, email FROM users WHERE id = ?;

-- sgl.query(GetUserAsString, one)
SELECT HEX(id) AS id_hex, email FROM users WHERE email = ?;
