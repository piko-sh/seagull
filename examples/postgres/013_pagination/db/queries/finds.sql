-- sgl.query(LogFind, exec)
INSERT INTO finds (creature, pool, size_mm) VALUES ($1, $2, $3);

-- sgl.query(ListFinds, many)
-- $1 as sgl.param(page_size, default: 3, max: 5)
-- $2 as sgl.param(page_offset)
SELECT id, creature, pool, size_mm
FROM finds
ORDER BY id
LIMIT $1 OFFSET $2;

-- sgl.query(SearchFinds, many)
-- $1 as sgl.param(creature, optional: true)
-- $2 as sgl.param(page_size, default: 3, max: 5)
-- $3 as sgl.param(page_offset)
SELECT id, creature, pool, size_mm
FROM finds
WHERE ($1::text IS NULL OR creature = $1)
ORDER BY size_mm DESC, id
LIMIT $2 OFFSET $3;
