-- sgl.query(Bumped, many)
SELECT id, n + $1 AS bumped FROM items;

-- sgl.query(Scaled, many)
SELECT id, i.price * $1 AS scaled FROM items i;

-- sgl.query(MaskedValues, many)
SELECT id, mask & $1 AS masked FROM items;

-- sgl.query(Distances, many)
SELECT id, location <-> $1 AS distance FROM items;

-- sgl.query(Shrink, many)
UPDATE items SET label = label WHERE id = $1 RETURNING id, n - $2 AS lowered;

-- sgl.query(CastBumped, many)
SELECT id, n + $1::numeric AS bumped FROM items;

-- sgl.query(BumpedFromTheLeft, many)
SELECT id, $1 + n AS bumped FROM items;

-- sgl.query(Labelled, many)
SELECT id, label || $1 AS labelled FROM items;
