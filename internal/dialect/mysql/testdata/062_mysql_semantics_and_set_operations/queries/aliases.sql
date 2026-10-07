-- sgl.query(name: BusyNames, command: many)
SELECT first_name, count(*) AS total, sum(visits) AS visit_total
FROM people
GROUP BY first_name
HAVING total > ? AND visit_total * 2 > ?
ORDER BY total * 2 DESC;
