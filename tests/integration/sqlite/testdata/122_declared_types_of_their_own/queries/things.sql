-- sgl.query(AddThing, exec)
INSERT INTO things (id, external_id, other_id, starts_at, zoned, span, raw, price, label)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- sgl.query(GetThing, one)
SELECT id, external_id, other_id, starts_at, zoned, span, raw, price, label FROM things WHERE external_id = ?;

-- sgl.query(ThingText, one)
SELECT lower(external_id) AS lowered, coalesce(starts_at, label) AS either, CAST(label AS UUID) AS converted
FROM things WHERE id = ?;
