-- sgl.query(AddReading, exec)
INSERT INTO readings (id, scores, label, flagged, tags) VALUES ($1, ARRAY[1, 2], $2, $3, ARRAY['a', 'b']);

-- sgl.query(AddJoinedReading, exec)
INSERT INTO readings (id, scores, label, flagged, tags) VALUES ($1, ARRAY[1, 2] || ARRAY[3], $2, $3, ARRAY['c'] || ARRAY['x', 'y']);

-- sgl.query(AddReadingPair, exec)
INSERT INTO readings (id, scores, label, flagged, tags)
VALUES ($1, ARRAY[4, 5], $2, $3, ARRAY['d']), ($4, ARRAY[(6), 7], $5, $6, ARRAY['e', 'f']);

-- sgl.query(SetReading, exec)
UPDATE readings SET (scores, label, flagged) = (ARRAY[8, 9], $1, $2) WHERE id = $3;

-- sgl.query(ListReadings, many)
SELECT id, scores, label, flagged, tags FROM readings ORDER BY id;
