-- sgl.query(name: Hourly, command: many)
SELECT time_bucket('1 hour', time) AS bucket, device_id, max(temperature) AS peak
FROM conditions
GROUP BY bucket, device_id
ORDER BY bucket;

-- sgl.query(name: Aligned, command: many)
SELECT time_bucket('15 minutes', time, '2000-01-01 00:07:00+00') AS bucket, count(*) AS readings
FROM conditions
GROUP BY bucket;

-- sgl.query(name: Daily, command: many)
SELECT bucket, device_id, avg_temp FROM conditions_daily WHERE bucket > now() - interval '7 days';

-- sgl.query(name: Percentiles, command: one)
SELECT approx_percentile(0.95, percentile_agg(temperature)) AS p95,
       approx_percentile(0.5, uddsketch(100, 0.01, temperature)) AS median
FROM conditions
WHERE device_id = $1;
