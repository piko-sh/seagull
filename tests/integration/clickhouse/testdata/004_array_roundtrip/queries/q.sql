-- sgl.query(Insert, exec)
INSERT INTO t (id, tags) VALUES ({id:UInt64}, {tags:Array(String)});

-- sgl.query(Get, one)
SELECT id, tags FROM t WHERE id = {id:UInt64};
