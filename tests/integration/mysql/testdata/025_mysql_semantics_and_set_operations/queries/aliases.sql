-- sgl.query(name: BusyNames, command: many)
SELECT first_name, count(*) AS total, max(visits) AS most_visits
FROM people
GROUP BY first_name
HAVING total > ? AND most_visits * 2 > ?
ORDER BY total * 2 DESC, first_name;
