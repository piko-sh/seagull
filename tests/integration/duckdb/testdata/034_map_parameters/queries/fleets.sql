-- sgl.query(name: AddFleet, command: exec)
INSERT INTO fleets (id, fees, extras) VALUES ($1, $2, $3);

-- sgl.query(name: GetFleet, command: one)
SELECT id, fees, extras FROM fleets WHERE id = $1;

-- sgl.query(name: FleetsWithFees, command: many)
SELECT id FROM fleets WHERE fees = $1 ORDER BY id;
