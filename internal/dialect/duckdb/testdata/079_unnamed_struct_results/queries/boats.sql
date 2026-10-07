-- sgl.query(BoatTuples, many)
SELECT ROW(id, name) AS pair, (id, name, 2.5) AS triple
FROM boats;

-- sgl.query(NestedTuples, many)
SELECT [ROW(id)] AS listed, {'pair': (id, name)} AS nested
FROM boats;

-- sgl.query(StarredTuples, many)
WITH pairs AS (SELECT ROW(id, name) AS pair FROM boats)
SELECT * FROM pairs;

-- sgl.query(RenamedTuple, one)
UPDATE boats SET name = $1 WHERE id = $2
RETURNING (id, name) AS pair;

-- sgl.query(ComparedTuples, many)
SELECT id
FROM boats
WHERE (id, name) = (1, 'Swift') AND ROW(id, name) <> ROW(2, 'Gull');
