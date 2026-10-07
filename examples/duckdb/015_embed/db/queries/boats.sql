-- sgl.query(AddHarbour, exec)
INSERT INTO harbours (name, county) VALUES ($1, $2);

-- sgl.query(AddSkipper, exec)
INSERT INTO skippers (name, licence) VALUES ($1, $2);

-- sgl.query(AddBoat, exec)
INSERT INTO boats (name, harbour_id, skipper_id) VALUES ($1, $2, $3);

-- sgl.query(GetBoat, one)
-- sgl.embed(harbours)
SELECT boats.id, boats.name, harbours.id, harbours.name, harbours.county
FROM boats
INNER JOIN harbours ON harbours.id = boats.harbour_id
WHERE boats.id = $1;

-- sgl.query(ListCrews, many)
-- sgl.embed(skippers)
SELECT boats.id, boats.name, skippers.id, skippers.name, skippers.licence
FROM boats
LEFT JOIN skippers ON skippers.id = boats.skipper_id
ORDER BY boats.id;
