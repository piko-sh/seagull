CREATE TABLE measurements (
    id BIGSERIAL,
    taken_on DATE NOT NULL,
    reading NUMERIC,
    PRIMARY KEY (id, taken_on)
) PARTITION BY RANGE (taken_on);
CREATE TABLE measurements_2026 PARTITION OF measurements FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE TABLE IF NOT EXISTS measurements_2027 PARTITION OF measurements (reading WITH OPTIONS NOT NULL, CONSTRAINT r CHECK (reading > 0)) FOR VALUES FROM ('2027-01-01') TO ('2028-01-01');
CREATE TABLE measurements_default PARTITION OF measurements DEFAULT;
ALTER TABLE measurements_2026 ADD CONSTRAINT positive CHECK (reading > 0);
ALTER TABLE measurements_2026 ALTER COLUMN reading SET DEFAULT 0;
CREATE INDEX measurements_2026_taken ON measurements_2026 (taken_on);
CREATE UNIQUE INDEX ON measurements_2027 (id, taken_on);
COMMENT ON TABLE measurements_2026 IS 'year 2026';
COMMENT ON COLUMN measurements_2026.reading IS 'a reading';
ALTER TABLE measurements ADD COLUMN note TEXT;
CREATE TABLE measurements_2028 (LIKE measurements INCLUDING ALL);
ALTER TABLE measurements ATTACH PARTITION measurements_2028 FOR VALUES FROM ('2028-01-01') TO ('2029-01-01');
ALTER TABLE measurements DETACH PARTITION measurements_2027;
ALTER TABLE measurements_2026 RENAME TO measurements_y2026;
ALTER TABLE measurements_y2026 ALTER COLUMN note SET NOT NULL;
GRANT SELECT ON measurements_y2026 TO PUBLIC;
DROP TABLE measurements_y2026;
DROP TABLE IF EXISTS measurements_default;
