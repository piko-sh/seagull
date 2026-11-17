-- sgl.query(name: CrewOfBoats, command: many)
SELECT boats.id, crew.id AS crew_id, crew.boat FROM boats, crew WHERE crew.boat = boats.name;

-- sgl.query(name: ThreeSources, command: many)
SELECT b.id, c.id AS crew_id FROM boats AS b, crew AS c, boats AS b2 WHERE b.id = b2.id;

-- sgl.query(name: CommaThenJoin, command: one)
SELECT count() AS pairings FROM boats, crew LEFT JOIN boats AS other ON other.id = crew.id;
