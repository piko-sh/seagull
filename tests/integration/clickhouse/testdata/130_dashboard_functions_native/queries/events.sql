-- sgl.query(name: Buckets, command: many)
SELECT toStartOfInterval(at, INTERVAL 15 MINUTE) AS quarter_hour, toStartOfInterval(day, INTERVAL 1 MONTH) AS month_start,
       toStartOfInterval(at_precise, INTERVAL 100 MILLISECOND) AS tick, count() AS events
FROM events
GROUP BY quarter_hour, month_start, tick
ORDER BY quarter_hour, tick;

-- sgl.query(name: Calendar, command: many)
SELECT id, toYYYYMM(day) AS month_key, toMonday(day) AS week_start, toLastDayOfMonth(at) AS month_end,
       toStartOfFiveMinutes(at) AS five_minutes, toDateTime(at_precise) AS whole_second, at + toIntervalHour(1) AS next_hour,
       YEAR(at) AS year, date_diff('day', day, toDate('2024-03-31')) AS days_left
FROM events
ORDER BY id;

-- sgl.query(name: Labels, command: many)
SELECT id, replace(path, '/', '-') AS slug, transform(kind, ['view', 'buy'], ['Viewed', 'Bought'], 'Other') AS label,
       formatReadableSize(bytes) AS size, splitByRegexp('/', path) AS segments
FROM events
ORDER BY id;

-- sgl.query(name: PerUser, command: many)
SELECT user_id, medianExact(bytes) AS median_bytes, BIT_AND(flags) AS common_flags, quantileTDigest(0.5)(duration) AS median_duration,
       windowFunnel(3600)(at, kind = 'view', kind = 'buy') AS reached, retention(kind = 'view', kind = 'buy') AS retained
FROM events
GROUP BY user_id
ORDER BY user_id;
