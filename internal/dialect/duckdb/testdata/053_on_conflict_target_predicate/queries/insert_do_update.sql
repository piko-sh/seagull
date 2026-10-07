-- sgl.query(name: InsertEventDoUpdate, command: one)
INSERT INTO events (slug, payload)
VALUES ($1, $2)
ON CONFLICT (slug) DO UPDATE SET payload = $3 WHERE archived = false
RETURNING id, slug, payload;
