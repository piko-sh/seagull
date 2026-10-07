-- sgl.query(EnterBoat, exec)
INSERT INTO entries (id, boat, crew, finish_seconds)
VALUES ({id:UInt64}, {boat:String}, {crew:UInt8}, {finish_seconds:UInt32});

-- sgl.query(ListEntries, many)
-- sgl.sortable(order_by, [boat, crew, finish_seconds])
SELECT id, boat, crew, finish_seconds FROM entries
