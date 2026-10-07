-- sgl.query(RingGull, one, optional: true)
INSERT INTO gulls (ring, species) VALUES (?, ?)
ON CONFLICT (ring) DO NOTHING
RETURNING id, ring;

-- sgl.query(FindGull, one, optional: true)
SELECT id, ring, species FROM gulls WHERE ring = ?;
