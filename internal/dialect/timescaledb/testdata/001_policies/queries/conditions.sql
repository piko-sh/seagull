-- sgl.query(name: HourlyConditions, command: many)
SELECT bucket, device_id, avg_temp FROM conditions_hourly ORDER BY bucket, device_id;

-- sgl.query(name: RecentConditions, command: many)
SELECT time, device_id, temperature FROM conditions WHERE time > $1 ORDER BY time;
