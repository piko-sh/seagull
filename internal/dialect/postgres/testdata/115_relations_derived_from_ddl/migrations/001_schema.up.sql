CREATE TABLE app.public.readings (
    id BIGINT NOT NULL,
    taken_on DATE NOT NULL,
    value NUMERIC,
    PRIMARY KEY (id, taken_on)
) PARTITION BY RANGE (taken_on);
CREATE TABLE readings_2026 PARTITION OF readings (value WITH OPTIONS NOT NULL) FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE TABLE readings_2027 PARTITION OF app.public.readings FOR VALUES FROM ('2027-01-01') TO ('2028-01-01');
ALTER TABLE readings ADD COLUMN note TEXT;
COMMENT ON COLUMN app.public.readings.note IS 'free text';

CREATE SEQUENCE app.public.reading_numbers START 100;

CREATE FUNCTION touch() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NEW; END$$;
CREATE OR REPLACE TRIGGER readings_touch BEFORE UPDATE ON readings FOR EACH ROW EXECUTE FUNCTION touch();
CREATE OR REPLACE TRIGGER readings_touch AFTER UPDATE ON readings FOR EACH ROW EXECUTE FUNCTION touch();
CREATE CONSTRAINT TRIGGER readings_check AFTER INSERT ON readings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION touch();

CREATE SCHEMA billing;
SET search_path TO billing, public;
CREATE TABLE invoices (id BIGINT PRIMARY KEY, reading_id BIGINT NOT NULL, total NUMERIC NOT NULL);
CREATE SEQUENCE invoice_numbers;
CREATE VIEW invoice_totals AS SELECT id, total FROM invoices;
RESET search_path;
CREATE TABLE audit_events (id BIGINT PRIMARY KEY);
