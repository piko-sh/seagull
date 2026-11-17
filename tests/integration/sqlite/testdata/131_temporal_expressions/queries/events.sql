-- sgl.query(LatestEvent, one)
SELECT max(created_at) AS latest FROM events;

-- sgl.query(EarliestEvent, one)
SELECT min(created_at) AS earliest FROM events;

-- sgl.query(CoalescedTimes, many)
SELECT coalesce(created_at, created_at) AS at FROM events ORDER BY id;

-- sgl.query(CastLabels, many)
SELECT CAST(label AS datetime) AS at FROM events ORDER BY id;

-- sgl.query(DirectTimes, many)
SELECT created_at FROM events ORDER BY id;
