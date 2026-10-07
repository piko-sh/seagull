-- sgl.query(Bumped, many)
SELECT id, n + $1 AS bumped FROM items ORDER BY id;

-- sgl.query(Scaled, many)
SELECT id, i.price * $1 AS scaled FROM items i ORDER BY id;

-- sgl.query(MaskedValues, many)
SELECT id, mask & $1 AS masked FROM items ORDER BY id;

-- sgl.query(Shrink, many)
UPDATE items SET mask = mask WHERE id = $1 RETURNING id, n - $2 AS lowered;
