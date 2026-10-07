-- sgl.query(RecordLanding, one)
INSERT INTO landings (catch_id, species, boxes, fish, weight_g, length_cm, sea_temp, price, on_ice, landed_on, landed_at, harbour_ip, tag, notes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id;

-- sgl.query(GetLanding, one)
SELECT id, catch_id, species, boxes, fish, weight_g, length_cm, sea_temp, price, on_ice, landed_on, landed_at, harbour_ip, tag, notes
FROM landings
WHERE id = $1;
