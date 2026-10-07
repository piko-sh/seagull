-- sgl.query(MoorBoat, execresult)
INSERT INTO boats (name, berth) VALUES (?, ?);

-- sgl.query(GetBoat, one)
SELECT id, name, berth, at_sea FROM boats WHERE id = ?;

-- sgl.query(ListBoats, many)
SELECT id, name, berth, at_sea FROM boats ORDER BY berth;

-- sgl.query(PutToSea, execrows)
UPDATE boats SET at_sea = TRUE WHERE at_sea = FALSE;

-- sgl.query(ScrapBoat, exec)
DELETE FROM boats WHERE id = ?;
