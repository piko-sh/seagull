-- sgl.migration(no_transaction: true)
CREATE MATERIALIZED VIEW hourly_readings
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 hour', ts) AS bucket,
    buoy,
    count(*) AS samples,
    stats_agg(temperature) AS stats
FROM readings
GROUP BY bucket, buoy;
