-- sgl.query(RecordLanding, one)
INSERT INTO landings (species, quay, boxes, fish, weight_g, length_cm, water_temp, on_ice, landed_at, landed_on, catch_id, auction_lot, tag, notes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id;

-- sgl.query(GetLanding, one)
SELECT id, species, quay, boxes, fish, weight_g, length_cm, water_temp, on_ice, landed_at, landed_on, catch_id, auction_lot, tag, notes
FROM landings
WHERE id = $1;
