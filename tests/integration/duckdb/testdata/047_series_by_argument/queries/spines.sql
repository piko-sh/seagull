-- sgl.query(name: DailySpine, command: many)
SELECT day, count(s.id) AS signups
FROM generate_series(TIMESTAMP '2024-01-01', TIMESTAMP '2024-01-03', INTERVAL 1 DAY) AS t(day)
LEFT JOIN signups s ON s.created_at >= day AND s.created_at < day + INTERVAL 1 DAY
GROUP BY day
ORDER BY day;

-- sgl.query(name: DateRange, command: many)
SELECT d FROM range(DATE '2024-02-27', DATE '2024-03-01', INTERVAL 1 DAY) AS t(d) ORDER BY d;

-- sgl.query(name: Counted, command: many)
SELECT n FROM range(3) AS t(n) ORDER BY n;

-- sgl.query(name: Tags, command: many)
SELECT s.id, tag FROM signups s, unnest(s.tags) AS t(tag) ORDER BY s.id, tag;
