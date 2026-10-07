-- sgl.query(RingGull, one, optional: true)
INSERT INTO gulls (ring, species) VALUES ($1, $2)
ON CONFLICT (ring) DO NOTHING
RETURNING id, ring;

-- sgl.query(FindGull, one, optional: true)
SELECT id, ring, species FROM gulls WHERE ring = $1;
