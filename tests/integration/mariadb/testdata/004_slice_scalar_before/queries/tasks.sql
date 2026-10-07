-- sgl.query(name: UpdateStatusByIDs, command: execrows)
-- ?2 as sgl.param(ids, kind: slice)
UPDATE tasks SET status = ?1 WHERE id IN (?2);

-- sgl.query(name: FetchByPriorityAndStatuses, command: many)
-- ?2 as sgl.param(statuses, kind: slice)
SELECT id, status
FROM tasks
WHERE priority = ?1 AND status IN (?2)
ORDER BY id ASC;
