-- sgl.query(name: FetchByPriorityAndStatuses, command: many)
-- $2 as sgl.param(statuses, kind: slice)
SELECT id, status, priority, title
FROM tasks
WHERE priority >= $1 AND status IN ($2)
ORDER BY id ASC;

-- sgl.query(name: CountByStatuses, command: one)
-- $1 as sgl.param(statuses, kind: slice)
SELECT COUNT(*) AS total FROM tasks WHERE status IN ($1);
