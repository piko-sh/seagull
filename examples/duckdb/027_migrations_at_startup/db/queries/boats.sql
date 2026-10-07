-- sgl.query(AddBoat, exec)
INSERT INTO boats (name, home_port) VALUES ($1, $2);

-- sgl.query(ListBoats, many)
SELECT id, name, home_port FROM boats ORDER BY id;
