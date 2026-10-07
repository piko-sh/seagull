-- sgl.query(AddBoat, exec)
INSERT INTO boats (name, home_port) VALUES ($1, $2);

-- sgl.query(ListBoats, many)
SELECT name, home_port FROM boats ORDER BY name;
