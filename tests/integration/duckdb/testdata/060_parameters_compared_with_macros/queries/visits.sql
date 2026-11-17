-- sgl.query(name: AddVisit, command: exec)
INSERT INTO visits (id, visitor, visited, stays) VALUES ($1, $2, $3, $4);

-- sgl.query(name: VisitsInYear, command: many)
SELECT id FROM visits WHERE year_of(visited) = $1 ORDER BY id;

-- sgl.query(name: VisitsByShout, command: many)
SELECT id FROM visits WHERE shout(visitor) = $1 ORDER BY id;

-- sgl.query(name: VisitsWithDoubledStays, command: many)
SELECT id FROM visits WHERE doubled(stays) = $1 ORDER BY id;

-- sgl.query(name: VisitsWithKeptStays, command: many)
SELECT id FROM visits WHERE kept(stays) = $1 ORDER BY id;
