-- sgl.query(ListFlights, many)
SELECT * FROM flights;

-- sgl.query(ListLegs, many)
SELECT legs.colony, flights.legs.minutes, `legs.altitude` FROM flights;

-- sgl.query(ListStops, many)
SELECT id, stops FROM routes;

-- sgl.query(ListColonies, many)
SELECT f.id, f.legs.colony AS colonies FROM flights AS f;

-- sgl.query(InsertFlight, exec)
INSERT INTO flights (id, `legs.colony`, `legs.minutes`, `legs.altitude`, note)
VALUES ({id:UInt64}, {colonies:Array(String)}, {minutes:Array(Nullable(UInt16))}, {altitudes:Array(UInt32)}, {note:String});
