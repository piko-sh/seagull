-- sgl.query(AddSailing, exec)
INSERT INTO sailings (origin, destination, departs, seats_free) VALUES ($1, $2, $3, $4);

-- sgl.query(FindSailings, many)
SELECT id, origin, destination, departs, seats_free
FROM sailings
WHERE origin = $1 AND destination = $2
ORDER BY departs;

-- sgl.query(SailingsDeparting, many)
-- $1 as sgl.param(earliest)
-- $2 as sgl.param(latest)
SELECT id, origin, destination, departs, seats_free
FROM sailings
WHERE departs >= $1 AND departs < $2
ORDER BY departs;

-- sgl.query(BookSeats, execrows)
UPDATE sailings
SET seats_free = seats_free - :seats
WHERE id = :sailing_id AND seats_free >= :seats;
