-- sgl.query(InsertEntry, exec)
INSERT INTO entries (id, tombstoned) VALUES ({id:UInt64}, {tombstoned:UInt8});

-- sgl.query(TombstoneAll, exec)
ALTER TABLE entries DELETE WHERE id = {id:UInt64};

-- sgl.query(Live, many)
SELECT id, tombstoned FROM entries WHERE tombstoned = 0 ORDER BY id;

-- sgl.query(AllRows, many)
SELECT id, tombstoned FROM entries ORDER BY id;
