-- sgl.query(AddTrip, exec)
INSERT INTO trips (name, destination, price_pence, dogs_welcome) VALUES ($1, $2, $3, $4);

-- sgl.query(SearchTrips, many)
-- $1 as sgl.param(destination, optional: true)
-- $2 as sgl.param(max_price, optional: true)
-- $3 as sgl.param(dogs_welcome, optional: true)
SELECT id, name, destination, price_pence, dogs_welcome
FROM trips
WHERE ($1::text IS NULL OR destination = $1)
  AND ($2::bigint IS NULL OR price_pence <= $2)
  AND ($3::boolean IS NULL OR dogs_welcome = $3)
ORDER BY price_pence, id;
