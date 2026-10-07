-- sgl.query(MoorBoat, one)
INSERT INTO boats (name, berth) VALUES ($1, $2) RETURNING id;

-- sgl.query(MoveBerth, execresult)
UPDATE boats SET berth = $1 WHERE id = $2;

-- sgl.query(GetBoat, one)
SELECT id, name, berth, at_sea FROM boats WHERE id = $1;

-- sgl.query(ListBoats, many)
SELECT id, name, berth, at_sea FROM boats ORDER BY berth;

-- sgl.query(PutToSea, execrows)
UPDATE boats SET at_sea = true WHERE NOT at_sea;

-- sgl.query(ScrapBoat, exec)
DELETE FROM boats WHERE id = $1;
