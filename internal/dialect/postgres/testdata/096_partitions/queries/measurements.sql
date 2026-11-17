-- sgl.query(name: ListMeasurements, command: many)
SELECT id, taken_on, reading, note FROM measurements WHERE taken_on >= $1;

-- sgl.query(name: ListArchived, command: many)
SELECT * FROM measurements_2027;

-- sgl.query(name: ListPartition, command: many)
SELECT * FROM measurements_2028;

-- sgl.query(name: ListNamedPartitionColumns, command: many)
SELECT p.id, p.reading, p.note FROM measurements_2028 AS p WHERE p.id = $1;
