-- sgl.query(InsertInventory, exec)
INSERT INTO inventory (id, counts) VALUES ({id:UInt64}, {counts:Map(String, UInt32)});

-- sgl.query(List, many)
SELECT id, counts FROM inventory ORDER BY id;
