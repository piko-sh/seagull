-- sgl.query(name: Labels, command: many)
SELECT transform(kind, ['view', 'click'], ['Viewed', 'Clicked'], 'Other') AS label,
       formatReadableSize(bytes) AS size, formatReadableTimeDelta(duration) AS took,
       splitByRegexp('/', path) AS segments, throwIf(bytes < 0, 'negative size') AS checked
FROM events;

-- sgl.query(name: Funnel, command: many)
SELECT user_id, windowFunnel(3600)(at, kind = 'view', kind = 'cart', kind = 'buy') AS reached,
       retention(kind = 'view', kind = 'buy') AS retained
FROM events
GROUP BY user_id;

-- sgl.query(name: Latency, command: one)
SELECT quantileTDigest(0.95)(duration) AS p95, quantileTiming(0.99)(duration) AS p99,
       quantilesTDigest(0.5, 0.9)(duration) AS spread
FROM events;
