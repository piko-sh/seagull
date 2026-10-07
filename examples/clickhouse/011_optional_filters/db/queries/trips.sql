-- sgl.query(AddTrip, exec)
INSERT INTO trips (id, name, destination, price_pence, dogs_welcome)
VALUES ({id:UInt64}, {name:String}, {destination:String}, {price_pence:UInt32}, {dogs_welcome:Bool});

-- sgl.query(SearchTrips, many)
-- {destination:Nullable(String)} as sgl.param(destination, optional: true)
-- {max_price:Nullable(UInt32)} as sgl.param(max_price, optional: true)
-- {dogs_welcome:Nullable(Bool)} as sgl.param(dogs_welcome, optional: true)
SELECT id, name, destination, price_pence, dogs_welcome
FROM trips
WHERE ({destination:Nullable(String)} IS NULL OR destination = {destination:Nullable(String)})
  AND ({max_price:Nullable(UInt32)} IS NULL OR price_pence <= {max_price:Nullable(UInt32)})
  AND ({dogs_welcome:Nullable(Bool)} IS NULL OR dogs_welcome = {dogs_welcome:Nullable(Bool)})
ORDER BY price_pence, id;
