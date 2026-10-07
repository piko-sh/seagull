-- sgl.query(name: FetchByStatusesLimited, command: many)
-- ?1 as sgl.param(statuses, kind: slice)
-- ?2 as sgl.param(page_size)
SELECT id, status, priority, title
FROM tasks
WHERE status IN (?1)
ORDER BY priority DESC, id ASC
LIMIT ?2;

-- sgl.query(name: CountByStatuses, command: one)
-- ?1 as sgl.param(statuses, kind: slice)
SELECT COUNT(*) AS total FROM tasks WHERE status IN (?1);
