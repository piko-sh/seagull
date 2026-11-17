-- sgl.query(InsertSource, exec)
INSERT INTO source (id, val) VALUES ({id:UInt64}, {val:UInt64});

-- sgl.query(CopySourceToDest, exec)
INSERT INTO dest (id, val) SELECT id, val * 10 FROM source;

-- sgl.query(ReadDest, many)
SELECT id, val FROM dest ORDER BY id;
