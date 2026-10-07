-- sgl.query(name: FetchByStatusesSorted, command: many)
-- ?1 as sgl.param(statuses, kind: slice)
-- sgl.sortable(order_by, columns: [id, priority, status])
SELECT id, status, priority, title
FROM tasks
WHERE status IN (?1)
