-- sgl.query(ByDefault, many)
SELECT g.id, s.beach, s.note FROM gulls AS g LEFT JOIN sightings AS s ON s.gull_id = g.id;

-- sgl.query(WithNulls, many)
SELECT g.id, s.beach, s.note FROM gulls AS g LEFT JOIN sightings AS s ON s.gull_id = g.id SETTINGS join_use_nulls = 1;

-- sgl.query(FullJoin, many)
SELECT g.id, s.beach FROM gulls AS g FULL OUTER JOIN sightings AS s ON s.gull_id = g.id SETTINGS join_use_nulls = 1;
