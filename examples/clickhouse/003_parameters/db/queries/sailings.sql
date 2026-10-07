-- sgl.query(AddSailing, exec)
INSERT INTO sailings (id, origin, destination, departs, seats_free)
VALUES ({id:UInt64}, {origin:String}, {destination:String}, {departs:String}, {seats_free:Int32});

-- sgl.query(FindSailings, many)
SELECT id, origin, destination, departs, seats_free
FROM sailings
WHERE origin = {origin:String} AND destination = {destination:String}
ORDER BY departs;

-- sgl.query(SailingsDeparting, many)
SELECT id, origin, destination, departs, seats_free
FROM sailings
WHERE departs >= {earliest:String} AND departs < {latest:String}
ORDER BY departs;

-- sgl.query(BookSeats, asyncexec)
ALTER TABLE sailings
UPDATE seats_free = seats_free - {seats:Int32}
WHERE id = {sailing_id:UInt64} AND seats_free >= {seats:Int32};

-- sgl.query(SeatsFree, one)
SELECT seats_free FROM sailings WHERE id = {sailing_id:UInt64};
