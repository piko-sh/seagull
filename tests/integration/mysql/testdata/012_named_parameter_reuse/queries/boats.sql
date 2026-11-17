-- sgl.query(name: AddBoat, command: exec)
INSERT INTO harbour_boats (id, name, nickname) VALUES (?, ?, ?);

-- sgl.query(name: FindBoatsByTerm, command: many)
SELECT id FROM harbour_boats WHERE name = :term OR nickname = :term OR CONCAT(name, '-', nickname) = :term ORDER BY id;

-- sgl.query(name: FindBoatsNumbered, command: many)
SELECT id FROM harbour_boats WHERE name = ?1 OR nickname = ?1 OR id = ?2 ORDER BY id;
