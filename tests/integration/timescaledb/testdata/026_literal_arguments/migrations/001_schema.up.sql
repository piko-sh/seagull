CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit;

CREATE TABLE conditions (
    time TIMESTAMPTZ NOT NULL,
    device_id TEXT NOT NULL,
    temperature DOUBLE PRECISION NOT NULL
);

SELECT create_hypertable('conditions', by_range('time'));

CREATE MATERIALIZED VIEW conditions_daily WITH (timescaledb.continuous) AS
SELECT time_bucket('1 day', time) AS bucket, device_id, max(temperature) AS peak
FROM conditions
GROUP BY bucket, device_id
WITH NO DATA;
