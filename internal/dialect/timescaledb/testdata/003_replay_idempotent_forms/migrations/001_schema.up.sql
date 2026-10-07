CREATE TABLE IF NOT EXISTS readings (device_id integer, taken_at timestamptz, value double precision, PRIMARY KEY (device_id, taken_at));
CREATE TABLE IF NOT EXISTS readings (id integer PRIMARY KEY);
ALTER TABLE readings ADD COLUMN IF NOT EXISTS value double precision;
CREATE VIEW reading_view (device, at, reading) AS SELECT * FROM readings;
