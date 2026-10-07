-- sgl.query(EnterBoat, exec)
INSERT INTO entries (boat, crew, finish_seconds) VALUES ($1, $2, $3);

-- sgl.query(ListEntries, many)
-- sgl.sortable(order_by, [boat, crew, finish_seconds])
SELECT id, boat, crew, finish_seconds FROM entries
