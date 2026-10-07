-- sgl.query(AddHarbour, exec)
INSERT INTO harbours (name, county) VALUES (?, ?);

-- sgl.query(AddSkipper, exec)
INSERT INTO skippers (name, licence) VALUES (?, ?);

-- sgl.query(AddBoat, exec)
INSERT INTO boats (name, harbour_id, skipper_id) VALUES (?, ?, ?);

-- sgl.query(GetBoat, one)
-- sgl.embed(harbours)
SELECT boats.id, boats.name, harbours.id, harbours.name, harbours.county
FROM boats
INNER JOIN harbours ON harbours.id = boats.harbour_id
WHERE boats.id = ?;

-- sgl.query(ListCrews, many)
-- sgl.embed(skippers)
SELECT boats.id, boats.name, skippers.id, skippers.name, skippers.licence
FROM boats
LEFT JOIN skippers ON skippers.id = boats.skipper_id
ORDER BY boats.id;
