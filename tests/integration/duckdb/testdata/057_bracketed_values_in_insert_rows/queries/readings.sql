-- sgl.query(AddReading, exec)
INSERT INTO readings (id, scores, label, flagged, tags) VALUES ($1, [1, 2], $2, $3, ['a', 'b']);

-- sgl.query(AddStructReading, exec)
INSERT INTO readings (id, scores, label, flagged, tags) VALUES ($1, {'a': [1, 2], 'b': 3}.a, $2, $3, {'x': ['c', 'd']}.x);

-- sgl.query(AddNestedReading, exec)
INSERT INTO readings (id, scores, label, flagged, tags) VALUES ($1, [[1, 2], [3]][1], $2, $3, map_keys(MAP {'e': 1, 'f': 2}));

-- sgl.query(AddReadingPair, exec)
INSERT INTO readings (id, scores, label, flagged, tags)
VALUES ($1, [4, 5], $2, $3, ['g']), ($4, [6] || [7], $5, $6, ['h', 'i']);

-- sgl.query(SetReading, exec)
UPDATE readings SET (scores, label, flagged) = ([8, 9], $1, $2) WHERE id = $3;

-- sgl.query(ListReadings, many)
SELECT id, scores, label, flagged, tags FROM readings ORDER BY id;
