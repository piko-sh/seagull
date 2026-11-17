-- sgl.query(name: Scores, command: many)
SELECT id, scores FROM readings ORDER BY id;

-- sgl.query(name: Limits, command: many)
SELECT id, limits FROM readings ORDER BY id;

-- sgl.query(name: FirstStages, command: many)
SELECT id, stages FROM readings WHERE id = 1;

-- sgl.query(name: Stages, command: many)
SELECT id, stages FROM readings ORDER BY id;

-- sgl.query(name: Notes, command: one)
SELECT list(note ORDER BY id) AS notes FROM readings;

-- sgl.query(name: Literal, command: many)
SELECT id, [label, NULL] AS pair FROM readings ORDER BY id;

-- sgl.query(name: StrictScores, command: many)
-- sgl.column(scores, non_null_elements: true)
SELECT id, scores FROM readings ORDER BY id;

-- sgl.query(name: StrictLimits, command: many)
-- sgl.column(limits, non_null_elements: true)
SELECT id, limits FROM readings ORDER BY id;

-- sgl.query(name: ListReadings, command: many)
SELECT * FROM readings ORDER BY id;
