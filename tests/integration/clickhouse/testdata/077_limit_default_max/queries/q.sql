-- sgl.query(ListFinds, many)
-- {page_size:UInt32} as sgl.param(page_size, default: 3, max: 5)
SELECT id, creature FROM finds ORDER BY id LIMIT {page_size:UInt32} OFFSET {page_offset:UInt32};
