-- sgl.query(BornOn, many)
SELECT id FROM people pe WHERE cast(pe.born AS text) = $1 AND length(pe.name) < $2;

-- sgl.query(BornOnPlain, many)
SELECT id FROM people WHERE CAST(born AS text) = $1;

-- sgl.query(BornAfter, many)
SELECT id FROM people WHERE CAST(born AS timestamp with time zone) > $1;

-- sgl.query(ListingsBelow, many)
SELECT id FROM reviews r WHERE cast(r.listing_id AS int) < $1;

-- sgl.query(NamedInParentheses, many)
SELECT id FROM people pe WHERE (pe.name) = $1;

-- sgl.query(Nearest, many)
SELECT id FROM places ORDER BY location <-> $1 LIMIT 5;

-- sgl.query(Masked, many)
SELECT id FROM places WHERE (mask & $1) <> 0;

-- sgl.query(SimilarLabels, many)
SELECT id FROM places WHERE label <-> $1 < 0.5;
