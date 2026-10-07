-- sgl.query(LogVoyage, exec)
INSERT INTO voyages (boat, ports, catch_kg, sightings) VALUES ($1, $2, $3, $4);

-- sgl.query(ListVoyages, many)
SELECT id, boat, ports, catch_kg, sightings FROM voyages ORDER BY id;

-- sgl.query(VoyagesCallingAt, many)
-- $1 as sgl.param(port)
SELECT boat, ports[1] AS first_port, cardinality(ports) AS stops
FROM voyages
WHERE $1::text = ANY(ports)
ORDER BY id;

-- sgl.query(VoyagesPerBoat, many)
SELECT boat, array_agg(id ORDER BY id) AS voyages
FROM voyages
GROUP BY boat
ORDER BY boat;
