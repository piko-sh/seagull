-- sgl.query(RecordReading, exec)
INSERT INTO readings (ts, buoy, temperature) VALUES ($1, $2, $3);

-- sgl.query(RefreshHourly, exec)
CALL refresh_continuous_aggregate('hourly_readings', NULL, NULL);

-- sgl.query(HourlyStats, many)
SELECT bucket, buoy, samples, average(stats) AS mean
FROM hourly_readings
ORDER BY bucket, buoy;

-- sgl.query(OverallStats, many)
SELECT
    buoy,
    num_vals(rollup(stats)) AS samples,
    avg(average(stats)) AS mean_of_hourly_means,
    average(rollup(stats)) AS mean,
    stddev(rollup(stats)) AS spread
FROM hourly_readings
GROUP BY buoy
ORDER BY buoy;
