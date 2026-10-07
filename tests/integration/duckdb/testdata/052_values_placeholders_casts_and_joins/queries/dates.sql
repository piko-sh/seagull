-- sgl.query(name: DaysSince, command: many)
SELECT id, date_diff('day', '2024-01-01', day) AS days, time_bucket('1 day', started) AS bucket FROM trips ORDER BY id;
