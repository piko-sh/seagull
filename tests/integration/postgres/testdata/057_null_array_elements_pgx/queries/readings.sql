-- sgl.query(name: Scores, command: many)
SELECT id, scores FROM readings ORDER BY id;

-- sgl.query(name: Notes, command: one)
SELECT array_agg(note ORDER BY id) AS notes FROM readings;

-- sgl.query(name: Literal, command: many)
SELECT id, ARRAY[label, NULL] AS pair FROM readings ORDER BY id;

-- sgl.query(name: StrictScores, command: many)
-- sgl.column(scores, non_null_elements: true)
SELECT id, scores FROM readings ORDER BY id;

-- sgl.query(name: ListReadings, command: many)
SELECT * FROM readings ORDER BY id;
