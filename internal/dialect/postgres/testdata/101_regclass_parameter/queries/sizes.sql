-- sgl.query(name: Relation, command: one)
SELECT $1::regclass AS relation;

-- sgl.query(name: ValueType, command: one)
SELECT pg_typeof(value) AS value_type, 'readings'::regclass AS relation FROM readings LIMIT 1;
