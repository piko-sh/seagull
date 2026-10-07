-- sgl.query(name: Hourly, command: many)
SELECT time_bucket('1 hour', time) AS bucket, device_id, max(temperature) AS peak
FROM conditions
GROUP BY bucket, device_id
ORDER BY bucket, device_id;

-- sgl.query(name: Aligned, command: many)
SELECT time_bucket('15 minutes', time, '2000-01-01 00:07:00+00'::timestamptz) AS bucket, count(*) AS readings
FROM conditions
GROUP BY bucket
ORDER BY bucket;

-- sgl.query(name: Daily, command: many)
SELECT bucket, device_id, peak FROM conditions_daily ORDER BY bucket, device_id;

-- sgl.query(name: Percentiles, command: one)
SELECT approx_percentile(0.5, percentile_agg(temperature)) AS median,
       approx_percentile(0.5, uddsketch(100, 0.01, temperature)) AS sketch_median
FROM conditions
WHERE device_id = $1;
