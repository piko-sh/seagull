-- sgl.query(name: ClaimJob, command: one)
UPDATE jobs
SET status = 'running', started_at = NOW()
WHERE id = $1
RETURNING *;

-- sgl.query(name: ClaimNextJobs, command: many)
WITH claimable AS (
    SELECT id FROM jobs
    WHERE status = 'pending'
    ORDER BY priority ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE jobs
SET status = 'running', started_at = NOW()
WHERE id IN (SELECT id FROM claimable)
RETURNING *;

-- sgl.query(name: InsertJob, command: one)
INSERT INTO jobs (job_type) VALUES ($1) RETURNING *;

-- sgl.query(name: DeleteCompletedJobs, command: many)
DELETE FROM jobs WHERE status = 'completed' RETURNING *;

-- sgl.query(name: ClaimQualifiedJob, command: one)
UPDATE jobs AS j SET status = 'running' WHERE j.id = $1 RETURNING j.*;

-- sgl.query(name: ClaimWithSubquery, command: many)
UPDATE jobs
SET status = 'running'
WHERE id IN (SELECT id FROM jobs WHERE status = 'pending' LIMIT $1)
RETURNING *;

-- sgl.query(name: ClaimWithSubqueryExplicitColumns, command: many)
UPDATE jobs
SET status = 'running'
WHERE id IN (SELECT id FROM jobs WHERE status = 'pending' LIMIT $1)
RETURNING id, job_type, status;

-- sgl.query(name: ClaimNextJobsNoLockHint, command: many)
WITH claimable AS (
    SELECT id FROM jobs
    WHERE status = 'pending'
    ORDER BY priority ASC
    LIMIT $1
)
UPDATE jobs
SET status = 'running', started_at = NOW()
WHERE id IN (SELECT id FROM claimable)
RETURNING *;

-- sgl.query(name: InsertJobWithDefaults, command: one)
-- $1 as sgl.param(priority, optional: true)
-- $2 as sgl.param(status, optional: true)
-- $3 as sgl.param(job_type)
INSERT INTO jobs (job_type, priority, status)
VALUES (
    $3,
    COALESCE($1::int, 5),
    COALESCE($2::text, 'pending')
) RETURNING *;

-- sgl.query(name: UpdateJobStatusWithCoalesce, command: exec)
-- $1 as sgl.param(started_at, optional: true)
-- $2 as sgl.param(attempts, optional: true)
-- $3 as sgl.param(status)
-- $4 as sgl.param(id)
UPDATE jobs
SET
    status = $3,
    started_at = COALESCE($1::timestamp, started_at),
    priority = COALESCE($2::int, priority)
WHERE id = $4;

-- sgl.query(name: ClaimNextJobsWithCasts, command: many)
WITH claimable AS (
    SELECT id FROM jobs
    WHERE status = 'pending'
    ORDER BY priority ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE jobs
SET status = 'running',
    started_at = $2::timestamp,
    priority = priority + 1
WHERE id IN (SELECT id FROM claimable)
RETURNING *;
