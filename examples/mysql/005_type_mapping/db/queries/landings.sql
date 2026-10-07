-- sgl.query(RecordLanding, execresult)
-- ?11 as sgl.param(price_per_kg, type: text)
INSERT INTO landings (species, boxes, fish, weight_g, length_cm, on_ice, landed_at, tag, notes, berth, price_per_kg)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- sgl.query(GetLanding, one)
SELECT id, species, boxes, fish, weight_g, length_cm, on_ice, landed_at, tag, notes, berth, price_per_kg
FROM landings
WHERE id = ?;
