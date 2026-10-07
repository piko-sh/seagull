-- sgl.query(AddToken, exec)
INSERT INTO tokens (id, owner, label) VALUES ($1, $2, $3);

-- sgl.query(SearchTokens, many)
-- $1 as sgl.param(owner, optional: true)
SELECT id FROM tokens WHERE ($1 IS NULL OR owner = $1) ORDER BY id;

-- sgl.query(name: TokensOf, command: many, dynamic: runtime)
SELECT id, label FROM tokens WHERE owner = $1

-- sgl.query(ListTokens, many)
SELECT id, owner, label FROM tokens ORDER BY id;
