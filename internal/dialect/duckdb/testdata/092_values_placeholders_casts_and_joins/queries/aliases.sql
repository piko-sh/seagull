-- sgl.query(name: Dearer, command: many)
SELECT id, fare * 2 AS doubled FROM trips WHERE doubled > ? ORDER BY doubled DESC;

-- sgl.query(name: BusyCities, command: many)
SELECT city, count(*) AS total FROM trips GROUP BY city HAVING total > ?;
