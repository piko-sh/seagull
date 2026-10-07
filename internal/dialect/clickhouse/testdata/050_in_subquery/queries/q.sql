-- sgl.query(InSubquery, many)
SELECT id FROM a WHERE id IN (SELECT id FROM b);
