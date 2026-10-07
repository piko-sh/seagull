-- sgl.query(name: HasIncompleteTasks, command: one)
-- ?1 as sgl.param(workflow_id)
SELECT EXISTS(
    SELECT 1 FROM orchestrator_tasks
    WHERE workflow_id = ?1 AND status NOT IN ('COMPLETE', 'FAILED')
) AS has_incomplete;
