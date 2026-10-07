-- sgl.query(name: Paths, command: many)
SELECT category || '/' || dir AS path FROM ledger WHERE path LIKE ? ORDER BY path;

-- sgl.query(name: Patterned, command: many)
SELECT id FROM ledger WHERE category LIKE concat("%", ?, "%");
