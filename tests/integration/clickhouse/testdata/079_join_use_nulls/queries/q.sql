-- sgl.query(SightingsByDefault, many)
SELECT g.id, s.beach, s.count, s.note
FROM gulls AS g
LEFT JOIN sightings AS s ON s.gull_id = g.id
ORDER BY g.id;

-- sgl.query(SightingsWithNulls, many)
SELECT g.id, s.beach, s.count, s.note
FROM gulls AS g
LEFT JOIN sightings AS s ON s.gull_id = g.id
ORDER BY g.id
SETTINGS join_use_nulls = 1;
