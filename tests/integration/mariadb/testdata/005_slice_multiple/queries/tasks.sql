-- sgl.query(name: FetchByStatusesAndPriorities, command: many)
-- ?1 as sgl.param(statuses, kind: slice)
-- ?2 as sgl.param(priorities, kind: slice)
SELECT id, status, priority
FROM tasks
WHERE status IN (?1) AND priority IN (?2)
ORDER BY id ASC;
