-- sgl.query(name: Riding, command: many)
SELECT r.id, r.city, r.name FROM (trips JOIN riders ON trips.id = riders.trip_id) AS r ORDER BY r.name;
