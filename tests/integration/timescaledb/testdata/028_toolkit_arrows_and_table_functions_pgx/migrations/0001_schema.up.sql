CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit CASCADE;

CREATE TABLE conditions (time timestamptz NOT NULL, device_id text NOT NULL, reading double precision);
