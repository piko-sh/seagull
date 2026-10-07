-- sgl.query(name: FetchByIDsAndStatus, command: many)
-- ?1 as sgl.param(ids, kind: slice)
SELECT id, status, title
FROM tasks
WHERE id IN (?1) AND status = ?2
ORDER BY id ASC;
