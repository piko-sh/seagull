-- sgl.query(AddHut, exec)
INSERT INTO huts (number, beach, colour) VALUES ({number:UInt64}, {beach:String}, {colour:String});

-- sgl.query(HutsOnBeaches, many)
SELECT number, beach, colour, booked
FROM huts
WHERE beach IN {beaches:Array(String)}
ORDER BY number;

-- sgl.query(BookHuts, asyncexec)
ALTER TABLE huts UPDATE booked = true WHERE number IN {numbers:Array(UInt64)} AND NOT booked;

-- sgl.query(CountBooked, one)
SELECT count() AS huts FROM huts WHERE booked;

-- sgl.query(FreeHutsInColours, many)
SELECT number, colour
FROM huts
WHERE beach = {beach:String} AND colour IN {colours:Array(String)} AND NOT booked
ORDER BY number;
