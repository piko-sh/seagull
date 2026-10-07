-- sgl.query(name: TripsFor, command: many)
SELECT id FROM trips WHERE city = :city AND id > $1 AND city <> :other ORDER BY id;
