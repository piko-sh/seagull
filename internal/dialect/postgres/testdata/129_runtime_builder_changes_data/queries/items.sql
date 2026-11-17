-- sgl.query(name: Purge, command: many, dynamic: runtime)
WITH gone AS (DELETE FROM items WHERE score < 0 RETURNING id, score) SELECT id, score FROM gone

-- sgl.query(name: Bump, command: many, dynamic: runtime)
WITH bumped AS (UPDATE items SET score = score + 1 RETURNING id, score) SELECT id, score FROM bumped
