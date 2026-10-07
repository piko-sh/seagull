-- sgl.query(BornOn, many)
SELECT id FROM people pe WHERE cast(pe.born AS text) = $1 AND length(pe.name) < $2 ORDER BY id;

-- sgl.query(BornOnPlain, many)
SELECT id FROM people WHERE CAST(born AS text) = $1 ORDER BY id;

-- sgl.query(ListingsBelow, many)
SELECT id FROM reviews r WHERE cast(r.listing_id AS int) < $1 ORDER BY id;

-- sgl.query(FlagsWith, many)
SELECT id FROM flags WHERE (mask & $1) <> 0 ORDER BY id;
