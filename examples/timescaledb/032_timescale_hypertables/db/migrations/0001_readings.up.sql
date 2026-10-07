CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE readings (
    ts          TIMESTAMPTZ      NOT NULL,
    buoy        TEXT             NOT NULL,
    temperature DOUBLE PRECISION NOT NULL
);

SELECT create_hypertable('readings', 'ts', chunk_time_interval => INTERVAL '1 day');
