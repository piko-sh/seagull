-- sgl.query(name: FetchByStatusesWithOptionalPriority, command: many)
-- ?1 as sgl.param(statuses, kind: slice)
-- ?2 as sgl.param(min_priority, optional: true)
SELECT id, status, priority, title
FROM tasks
WHERE status IN (?1) AND (?2 IS NULL OR priority >= ?2)
ORDER BY id ASC;
