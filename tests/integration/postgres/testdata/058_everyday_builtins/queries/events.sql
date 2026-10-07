-- sgl.query(name: Ages, command: many)
SELECT id, age(created_at, '2024-01-01 00:00:00+00') AS since_new_year, date_bin('15 minutes', happened, '2000-01-01') AS slot,
       lower(booked) AS starts, upper(booked) AS ends
FROM events
ORDER BY id;

-- sgl.query(name: Numbers, command: one)
SELECT cbrt(27.0) AS cube_root, sqrt(amount) AS exact_root, percentile_cont(0.5) WITHIN GROUP (ORDER BY ratio) AS median_ratio,
       corr(ratio, id) AS correlation, regr_count(ratio, id) AS pairs, any_value(feeling) AS some_feeling
FROM events
GROUP BY amount
ORDER BY amount
LIMIT 1;

-- sgl.query(name: Texts, command: many)
SELECT id, regexp_replace(body, '\s+', ' ', 'g') AS squeezed, regexp_match(body, '(\d+)') AS first_number,
       regexp_split_to_array(body, ',') AS parts, regexp_count(body, 'a') AS a_count, regexp_substr(body, '\d+') AS digits,
       encode(sha256(raw), 'hex') AS digest, convert_from(raw, 'UTF8') AS decoded, to_hex(id) AS hex_id
FROM events
ORDER BY id;

-- sgl.query(name: Documents, command: many)
SELECT id, jsonb_path_query_first(payload, '$.items[0]') AS first_item, jsonb_set(payload, tags, '1') AS tagged,
       jsonb_object_keys(payload) AS key
FROM events
ORDER BY id, key;

-- sgl.query(name: MarkDone, command: exec)
UPDATE events SET payload = jsonb_set(payload, '{status}', '"done"') WHERE id = $1;

-- sgl.query(name: Statuses, command: many)
SELECT id, payload ->> 'status' AS status FROM events ORDER BY id;

-- sgl.query(name: Series, command: many)
SELECT generate_series(1, 2) AS n, generate_series('2024-01-01'::timestamptz, '2024-01-02'::timestamptz, interval '1 day') AS day;

-- sgl.query(name: Moods, command: one)
SELECT enum_first(NULL::mood) AS lowest, enum_last(feeling) AS highest, enum_range(NULL::mood) AS every_mood
FROM events
WHERE id = $1;

-- sgl.query(name: NewID, command: one)
SELECT gen_random_uuid() IS NOT NULL AS generated;

-- sgl.query(name: LockAccount, command: exec)
SELECT pg_advisory_xact_lock($1::bigint);

-- sgl.query(name: TryLock, command: one)
SELECT pg_try_advisory_xact_lock($1::bigint) AS locked;
