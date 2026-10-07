-- sgl.query(AddEvent, exec)
INSERT INTO events (id, happened, precise, micro, day) VALUES (?, ?, ?, ?, ?);

-- sgl.query(GetEvent, one)
SELECT id, happened, precise, micro, day FROM events WHERE id = ?;

-- sgl.query(EventsAt, many)
SELECT id FROM events WHERE precise = ? ORDER BY id;

-- sgl.query(EventCasts, one)
SELECT CAST(precise AS DATETIME) AS precise_year, CAST(day AS DATE) AS day_year, CAST(happened AS TIMESTAMP) AS happened_year,
       CAST('12.5' AS TIMESTAMP(3)) AS fraction, CAST(micro AS DATETIME(6)) AS micro_year
FROM events WHERE id = ?;
