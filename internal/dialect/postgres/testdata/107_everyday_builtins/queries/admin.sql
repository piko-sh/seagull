-- sgl.query(name: NewID, command: one)
SELECT gen_random_uuid() AS id;

-- sgl.query(name: LockAccount, command: exec)
SELECT pg_advisory_xact_lock($1::bigint);

-- sgl.query(name: TryLock, command: one)
SELECT pg_try_advisory_lock($1::bigint) AS locked;

-- sgl.query(name: Notify, command: exec)
SELECT pg_notify('events', $1);

-- sgl.query(name: Setting, command: one)
SELECT current_setting('search_path') AS path, current_setting('app.tenant', true) AS tenant,
       pg_size_pretty(1024::bigint) AS size;

-- sgl.query(name: Statistics, command: one)
SELECT corr(ratio, amount) AS correlation, regr_slope(ratio, amount) AS slope, regr_count(ratio, amount) AS pairs,
       any_value(body) AS sample, string_agg(raw, '\x00'::bytea) AS joined
FROM events;
