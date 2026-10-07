-- sgl.query(name: ListJobs, command: many)
SELECT id, state FROM jobs WHERE state = $1;

-- sgl.query(name: LockJob, command: one)
SELECT id, state FROM jobs WHERE id = $1 FOR UPDATE;

-- sgl.query(name: LockInCTE, command: one)
WITH target AS (SELECT id, state FROM jobs WHERE id = $1 FOR UPDATE)
SELECT id, state FROM target;

-- sgl.query(name: LockInDerivedTable, command: many)
SELECT d.id FROM (SELECT id FROM jobs WHERE state = $1 FOR NO KEY UPDATE) d;

-- sgl.query(name: LockInSubquery, command: many)
SELECT id FROM jobs WHERE id IN (SELECT id FROM jobs WHERE state = $1 FOR UPDATE SKIP LOCKED) LIMIT 1;

-- sgl.query(name: LockInExists, command: many)
SELECT j.id FROM jobs j WHERE EXISTS (SELECT 1 FROM jobs k WHERE k.owner = j.owner FOR SHARE);

-- sgl.query(name: LockInScalarSubquery, command: many)
SELECT j.id, (SELECT k.state FROM jobs k WHERE k.id = j.id FOR KEY SHARE) AS locked_state FROM jobs j;

-- sgl.query(name: NoLockInCTE, command: one)
WITH target AS (SELECT id, state FROM jobs WHERE id = $1)
SELECT id, state FROM target;
