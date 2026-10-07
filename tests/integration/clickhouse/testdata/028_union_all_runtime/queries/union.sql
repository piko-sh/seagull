-- sgl.query(InsertA, exec)
INSERT INTO a (id, label) VALUES ({id:UInt64}, {label:String});

-- sgl.query(InsertB, exec)
INSERT INTO b (id, label) VALUES ({id:UInt64}, {label:String});

-- sgl.query(Both, many)
SELECT id, label FROM (
    SELECT id, label FROM a
    UNION ALL
    SELECT id, label FROM b
) ORDER BY id, label;
