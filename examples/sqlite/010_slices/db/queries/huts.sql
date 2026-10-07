-- sgl.query(AddHut, exec)
INSERT INTO huts (number, beach, colour) VALUES (?, ?, ?);

-- sgl.query(HutsOnBeaches, many)
-- ?1 as sgl.param(beaches, kind: slice)
SELECT number, beach, colour, booked
FROM huts
WHERE beach IN (?1)
ORDER BY number;

-- sgl.query(BookHuts, execrows)
-- ?1 as sgl.param(numbers, kind: slice)
UPDATE huts SET booked = 1 WHERE number IN (?1) AND booked = 0;

-- sgl.query(FreeHutsInColours, many)
-- ?1 as sgl.param(beach)
-- ?2 as sgl.param(colours, kind: slice)
SELECT number, colour
FROM huts
WHERE beach = ?1 AND colour IN (?2) AND booked = 0
ORDER BY number;
