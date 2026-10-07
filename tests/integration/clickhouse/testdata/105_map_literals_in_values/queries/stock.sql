-- sgl.query(RecordStock, exec)
INSERT INTO stock (id, counts, shelves, tagged, nested)
VALUES ({id:UInt64}, {'apples': 5, 'pears}': 2}, [{'top': 1}, {}], ({'b': 2}, 3), {'c': {'d': 4}});

-- sgl.query(RecordRows, exec)
INSERT INTO stock (id, counts) VALUES (2, {}), ({id:UInt64}, map('kiwis', {kiwis:UInt32}));

-- sgl.query(RestockMany, batch)
INSERT INTO stock (id, counts) VALUES ({id:UInt64}, {'restocked': 1});

-- sgl.query(LoadRows, exec)
INSERT INTO stock FORMAT JSONEachRow {"id": 9, "counts": {"figs": 1}}

-- sgl.query(ListStock, many)
SELECT id, counts, shelves, tagged, nested FROM stock ORDER BY id;
