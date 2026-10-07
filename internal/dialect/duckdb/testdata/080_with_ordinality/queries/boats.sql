-- sgl.query(NumberedScores, many)
SELECT boats.id, u.score, u.position
FROM boats, unnest(boats.scores) WITH ORDINALITY AS u(score, position);

-- sgl.query(NumberedRange, many)
SELECT * FROM range(3) WITH ORDINALITY AS r;

-- sgl.query(NumberedSeries, many)
SELECT ordinality, generate_series FROM generate_series(1, 3) WITH ORDINALITY;

-- sgl.query(LateralPositions, many)
SELECT n.position FROM boats, LATERAL unnest(boats.scores) WITH ORDINALITY AS n(score, position);
