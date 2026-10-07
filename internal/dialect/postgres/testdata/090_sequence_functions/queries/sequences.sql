-- sgl.query(name: NextCounterValue, command: one)
SELECT nextval('counters_id_seq') AS next_id;

-- sgl.query(name: ResetCounter, command: one)
SELECT setval('counters_id_seq', 1) AS reset_value;

-- sgl.query(name: CountRows, command: one)
SELECT COUNT(*) AS total FROM counters;
