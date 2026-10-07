-- sgl.query(name: GetEventCount, command: one)
SELECT count_events() AS total;

-- sgl.query(name: ListEvents, command: many)
SELECT id, name, occurred_at FROM events;
