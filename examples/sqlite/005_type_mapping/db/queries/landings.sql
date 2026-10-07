-- sgl.query(RecordLanding, one)
INSERT INTO landings (species, boxes, fish, weight_g, length_cm, on_ice, landed_at, tag, notes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- sgl.query(GetLanding, one)
SELECT id, species, boxes, fish, weight_g, length_cm, on_ice, landed_at, tag, notes
FROM landings
WHERE id = ?;
