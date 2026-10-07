-- sgl.query(AddSailing, exec)
INSERT INTO sailings (origin, destination, departs, seats_free) VALUES (?, ?, ?, ?);

-- sgl.query(FindSailings, many)
SELECT id, origin, destination, departs, seats_free
FROM sailings
WHERE origin = ? AND destination = ?
ORDER BY departs;

-- sgl.query(SailingsDeparting, many)
-- ?1 as sgl.param(earliest)
-- ?2 as sgl.param(latest)
SELECT id, origin, destination, departs, seats_free
FROM sailings
WHERE departs >= ?1 AND departs < ?2
ORDER BY departs;

-- sgl.query(BookSeats, execrows)
-- ?1 as sgl.param(seats)
-- ?2 as sgl.param(sailing_id)
UPDATE sailings
SET seats_free = seats_free - ?1
WHERE id = ?2 AND seats_free >= ?1;
