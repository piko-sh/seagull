-- sgl.query(InsertItem, exec)
INSERT INTO items (id, name) VALUES ({id:UInt64}, {name:String});

-- sgl.query(GetItemsByIDs, many)
SELECT id, name FROM items WHERE id IN {ids:Array(UInt64)} ORDER BY id;
