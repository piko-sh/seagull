-- sgl.query(AddHut, exec)
INSERT INTO huts (number, beach, colour) VALUES ($1, $2, $3);

-- sgl.query(HutsOnBeaches, many)
-- $1 as sgl.param(beaches, kind: slice)
SELECT number, beach, colour, booked
FROM huts
WHERE beach IN ($1)
ORDER BY number;

-- sgl.query(BookHuts, execrows)
-- $1 as sgl.param(numbers, kind: slice)
UPDATE huts SET booked = true WHERE number IN ($1) AND NOT booked;

-- sgl.query(FreeHutsInColours, many)
-- $1 as sgl.param(beach)
-- $2 as sgl.param(colours, kind: slice)
SELECT number, colour
FROM huts
WHERE beach = $1 AND colour IN ($2) AND NOT booked
ORDER BY number;
