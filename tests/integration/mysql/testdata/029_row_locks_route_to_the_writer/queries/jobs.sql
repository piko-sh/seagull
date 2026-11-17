-- sgl.query(AddJob, exec)
INSERT INTO jobs (id, state, owner) VALUES (?, ?, ?);

-- sgl.query(ListJobs, many)
SELECT id, state FROM jobs WHERE state = ? ORDER BY id;

-- sgl.query(LockJob, one)
SELECT id, state FROM jobs WHERE id = ? FOR UPDATE;

-- sgl.query(ShareLockJob, one)
SELECT id, state FROM jobs WHERE id = ? LOCK IN SHARE MODE;

-- sgl.query(LockInCTE, one)
WITH target AS (SELECT id, state FROM jobs WHERE id = ? FOR UPDATE)
SELECT id, state FROM target;

-- sgl.query(LockInDerivedTable, many)
SELECT d.id FROM (SELECT id FROM jobs WHERE state = ? FOR SHARE) d ORDER BY d.id;

-- sgl.query(LockInSubquery, many)
SELECT id FROM jobs WHERE id IN (SELECT id FROM jobs WHERE state = ? FOR UPDATE SKIP LOCKED) ORDER BY id;

-- sgl.query(LockInExists, many)
SELECT j.id FROM jobs j WHERE EXISTS (SELECT 1 FROM jobs k WHERE k.owner = j.owner AND k.id <> j.id LOCK IN SHARE MODE) ORDER BY j.id;

-- sgl.query(LockInScalarSubquery, many)
SELECT j.id, (SELECT k.state FROM jobs k WHERE k.id = j.id FOR UPDATE) AS locked_state FROM jobs j ORDER BY j.id;
