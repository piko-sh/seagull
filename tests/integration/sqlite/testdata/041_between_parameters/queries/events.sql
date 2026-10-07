-- sgl.query(name: GetEventsBetweenDates, command: many)
-- ?1 as sgl.param(start_date)
-- ?2 as sgl.param(end_date)
SELECT id, name, event_date FROM events WHERE event_date BETWEEN ?1 AND ?2 ORDER BY event_date;
