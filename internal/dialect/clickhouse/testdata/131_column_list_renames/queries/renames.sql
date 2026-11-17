-- sgl.query(name: ListedCTE, command: many)
WITH named(reading_id, reading_label) AS (SELECT id, label FROM readings)
SELECT reading_id, reading_label FROM named;

-- sgl.query(name: ListedDerived, command: many)
SELECT reading_id, reading_label FROM (SELECT id, label FROM readings) AS named(reading_id, reading_label);

-- sgl.query(name: ListedDerivedWithoutAs, command: many)
SELECT named.reading_id FROM (SELECT id, label FROM readings) named(reading_id, reading_label);
