-- sgl.query(name: ListActiveEvents, command: many)
SELECT id, name, event_date FROM active_events ORDER BY event_date;

-- sgl.query(name: GetActiveEvent, command: one)
SELECT id, name, event_date FROM active_events WHERE id = ?;
