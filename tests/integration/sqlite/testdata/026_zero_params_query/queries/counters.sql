-- sgl.query(name: CountAll, command: one)
SELECT count(*) AS total FROM counters;

-- sgl.query(name: ListCounters, command: many)
SELECT id, name, value FROM counters ORDER BY id;
