-- sgl.query(LogFind, exec)
INSERT INTO finds (id, creature, pool, size_mm)
VALUES ({id:UInt64}, {creature:String}, {pool:String}, {size_mm:UInt32});

-- sgl.query(ListFinds, many)
-- {page_size:UInt32} as sgl.param(page_size, default: 3, max: 5)
SELECT id, creature, pool, size_mm
FROM finds
ORDER BY id
LIMIT {page_size:UInt32} OFFSET {page_offset:UInt32};

-- sgl.query(SearchFinds, many)
-- {creature:Nullable(String)} as sgl.param(creature, optional: true)
-- {page_size:UInt32} as sgl.param(page_size, default: 3, max: 5)
SELECT id, creature, pool, size_mm
FROM finds
WHERE ({creature:Nullable(String)} IS NULL OR creature = {creature:Nullable(String)})
ORDER BY size_mm DESC, id
LIMIT {page_size:UInt32} OFFSET {page_offset:UInt32};
