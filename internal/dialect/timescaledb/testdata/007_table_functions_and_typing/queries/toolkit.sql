-- sgl.query(name: MeanReadings, command: many)
SELECT device_id, stats_agg(reading) -> average() AS mean, stats_agg(reading) -> stddev('population') AS spread,
    stats_agg(reading) -> num_vals() AS readings FROM conditions GROUP BY device_id;

-- sgl.query(name: Percentiles, command: one)
SELECT percentile_agg(reading) -> approx_percentile(0.95) AS p95, percentile_agg(reading) -> num_vals() AS sampled FROM conditions;

-- sgl.query(name: Points, command: many)
SELECT * FROM unnest((SELECT timevector(time, reading) FROM conditions));

-- sgl.query(name: Durations, command: many)
SELECT * FROM into_values((SELECT state_agg(time, device_id) FROM conditions));
