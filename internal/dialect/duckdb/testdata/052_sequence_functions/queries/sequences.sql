-- sgl.query(name: NextCounterValue, command: one)
SELECT nextval('counters_seq') AS next_id;

-- sgl.query(name: CountRows, command: one)
SELECT COUNT(*) AS total FROM counters;
