-- sgl.query(name: ListedCTE, command: many)
WITH named(reading_id, reading_label) AS (SELECT id, label FROM readings)
SELECT reading_id, reading_label FROM named;

-- sgl.query(name: ListedDerived, command: many)
SELECT reading_id, reading_label FROM (SELECT id, label FROM readings) AS named(reading_id, reading_label);

-- sgl.query(name: ShortCTE, command: many)
WITH named(reading_id) AS (SELECT id, label FROM readings)
SELECT reading_id FROM named;

-- sgl.query(name: ShortDerived, command: many)
SELECT reading_id FROM (SELECT id, label FROM readings) AS named(reading_id);
