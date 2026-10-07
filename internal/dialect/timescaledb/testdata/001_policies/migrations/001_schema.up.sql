CREATE TABLE conditions (
    time TIMESTAMPTZ NOT NULL,
    device_id TEXT NOT NULL,
    reading_id BIGINT NOT NULL,
    temperature DOUBLE PRECISION
);

SELECT create_hypertable('conditions', by_range('time', INTERVAL '1 day'));

SELECT set_chunk_time_interval('conditions', INTERVAL '2 days');

SELECT add_dimension('conditions', by_hash('device_id', 4));

CREATE INDEX conditions_device_time_idx ON conditions (device_id, time DESC);

SELECT add_reorder_policy('conditions', 'conditions_device_time_idx');

ALTER TABLE conditions SET (timescaledb.compress, timescaledb.compress_segmentby = 'device_id');

SELECT add_compression_policy('conditions', INTERVAL '7 days');

SET timescaledb.enable_chunk_skipping = on;

SELECT enable_chunk_skipping('conditions', 'reading_id');

SELECT add_retention_policy('conditions', INTERVAL '90 days');

CREATE MATERIALIZED VIEW conditions_hourly WITH (timescaledb.continuous) AS
SELECT time_bucket(INTERVAL '1 hour', time) AS bucket, device_id, avg(temperature) AS avg_temp
FROM conditions
GROUP BY bucket, device_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy('conditions_hourly',
    start_offset => INTERVAL '3 hours',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour');

SELECT add_retention_policy('conditions_hourly', INTERVAL '1 year');

ALTER MATERIALIZED VIEW conditions_hourly SET (timescaledb.compress = true);

SELECT add_compression_policy('conditions_hourly', compress_after => INTERVAL '30 days');

SELECT remove_compression_policy('conditions_hourly');

SELECT remove_retention_policy('conditions_hourly');

SELECT remove_continuous_aggregate_policy('conditions_hourly');

SELECT disable_chunk_skipping('conditions', 'reading_id');

SELECT remove_retention_policy('conditions');

SELECT remove_compression_policy('conditions');

SELECT remove_reorder_policy('conditions');

CREATE TABLE metrics (
    ts TIMESTAMPTZ NOT NULL,
    device TEXT NOT NULL,
    value DOUBLE PRECISION NOT NULL
);

SELECT create_hypertable('metrics', by_range('ts'));

ALTER TABLE metrics SET (timescaledb.enable_columnstore = true, timescaledb.segmentby = 'device');

CALL add_columnstore_policy('metrics', after => INTERVAL '1 day');

CALL remove_columnstore_policy('metrics');
