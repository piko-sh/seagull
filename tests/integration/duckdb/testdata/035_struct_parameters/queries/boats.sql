-- sgl.query(name: MoorBoat, command: exec)
INSERT INTO boats (id, berth, route, extra) VALUES ($1, $2, $3, $4);

-- sgl.query(name: GetBoat, command: one)
SELECT id, berth, route, extra FROM boats WHERE id = $1;

-- sgl.query(name: BoatsAtBerth, command: many)
SELECT id FROM boats WHERE berth = $1 ORDER BY id;
