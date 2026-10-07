-- sgl.query(name: ListJobs, command: many)
SELECT id, state FROM jobs WHERE state = ?;

-- sgl.query(name: ShareLockJob, command: one)
SELECT id, state FROM jobs WHERE id = ? LOCK IN SHARE MODE;

-- sgl.query(name: LockInCTE, command: one)
WITH target AS (SELECT id, state FROM jobs WHERE id = ? FOR UPDATE)
SELECT id, state FROM target;

-- sgl.query(name: LockInDerivedTable, command: many)
SELECT d.id FROM (SELECT id FROM jobs WHERE state = ? FOR SHARE) d;

-- sgl.query(name: LockInSubquery, command: many)
SELECT id FROM jobs WHERE id IN (SELECT id FROM jobs WHERE state = ? FOR UPDATE SKIP LOCKED);

-- sgl.query(name: LockInExists, command: many)
SELECT j.id FROM jobs j WHERE EXISTS (SELECT 1 FROM jobs k WHERE k.owner = j.owner LOCK IN SHARE MODE);

-- sgl.query(name: LockInScalarSubquery, command: many)
SELECT j.id, (SELECT k.state FROM jobs k WHERE k.id = j.id FOR UPDATE) AS locked_state FROM jobs j;

-- sgl.query(name: NoLockInCTE, command: one)
WITH target AS (SELECT id, state FROM jobs WHERE id = ?)
SELECT id, state FROM target;
