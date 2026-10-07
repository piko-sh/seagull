-- sgl.query(name: FindGull, command: one)
SELECT id, name FROM gulls WHERE id = ? AND name = ?;

-- sgl.query(name: FindGullMixed, command: many)
SELECT id, colony FROM gulls WHERE colony = $1 OR name = ?;

-- sgl.query(name: FindGullNumbered, command: many)
SELECT id FROM gulls WHERE name = ?2 AND id > ?1;
