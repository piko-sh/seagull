-- sgl.query(name: NewestAndOldest, command: many)
(SELECT id, first_name FROM people WHERE visits > ? ORDER BY id DESC LIMIT 1)
UNION ALL
(SELECT id, first_name FROM archived_people ORDER BY id LIMIT 1)
ORDER BY id;

-- sgl.query(name: WithDefaults, command: many)
VALUES ROW(0, 'nobody') UNION ALL SELECT id, first_name FROM people WHERE id = ?;

-- sgl.query(name: Countdown, command: many)
WITH RECURSIVE countdown (n) AS (VALUES ROW(3) UNION ALL SELECT n - 1 FROM countdown WHERE n > 1)
SELECT n FROM countdown;

-- sgl.query(name: Everyone, command: many)
SELECT everyone.id FROM ((SELECT id FROM people) UNION (SELECT id FROM archived_people)) AS everyone
WHERE everyone.id > ?;

-- sgl.query(name: FromTheView, command: many)
SELECT id, first_name FROM newest_and_oldest;
