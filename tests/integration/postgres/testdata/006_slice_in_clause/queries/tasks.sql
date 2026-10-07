-- sgl.query(name: FetchByStatuses, command: many)
-- $1 as sgl.param(statuses, kind: slice)
SELECT id, status, priority, title
FROM tasks
WHERE status IN ($1)
ORDER BY priority DESC, id ASC;

-- sgl.query(name: FetchByStatusesAndPriority, command: many)
-- $1 as sgl.param(statuses, kind: slice)
SELECT id, status, priority, title
FROM tasks
WHERE status IN ($1) AND priority >= $2
ORDER BY id ASC;

-- sgl.query(name: DeleteByIDs, command: execrows)
-- $1 as sgl.param(ids, kind: slice)
DELETE FROM tasks WHERE id IN ($1);

-- sgl.query(name: CountByStatuses, command: one)
-- $1 as sgl.param(statuses, kind: slice)
SELECT COUNT(*) AS total FROM tasks WHERE status IN ($1);
