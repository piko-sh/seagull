-- sgl.query(name: DailySpine, command: many)
SELECT day::date AS day, count(s.id) AS signups
FROM generate_series('2024-01-01'::timestamptz, '2024-01-03'::timestamptz, interval '1 day') AS day
LEFT JOIN signups s ON s.created_at >= day AND s.created_at < day + interval '1 day'
GROUP BY day
ORDER BY day;

-- sgl.query(name: LocalSpine, command: many)
SELECT moment FROM generate_series($1::timestamp, $2::timestamp, '6 hours') AS moment ORDER BY moment;

-- sgl.query(name: DateSpine, command: many)
SELECT d FROM generate_series(DATE '2024-02-27', DATE '2024-03-01', interval '1 day') AS d ORDER BY d;

-- sgl.query(name: Counted, command: many)
SELECT n, big FROM generate_series(1, 3) AS n, generate_series(1, 2::bigint) AS big ORDER BY n, big;

-- sgl.query(name: Tags, command: many)
SELECT s.id, tag FROM signups s, unnest(s.tags) AS tag ORDER BY s.id, tag;
