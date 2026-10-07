-- sgl.query(HighScoringBoats, many)
SELECT id
FROM boats
WHERE len(list_filter(scores, score -> score > $1)) > 0
  AND score = $2;

-- sgl.query(BoatsWithHighScores, many)
SELECT id
FROM boats
GROUP BY id
HAVING len(list_filter(max(scores), score -> score > $1)) > 0;

-- sgl.query(BoatsWithComprehendedScores, many)
SELECT id
FROM boats
WHERE len([score FOR score IN scores IF score > $1]) > 0;

-- sgl.query(BoatsWithOlderCrew, many)
SELECT id
FROM boats
WHERE len(list_filter(crew, tag -> tag.age > $1)) > 0;

-- sgl.query(RenameHighScoringBoats, execrows)
UPDATE boats
SET score = $1
WHERE len(list_filter(scores, lambda score: score > $2)) > 0;

-- sgl.query(DropScore, execrows)
UPDATE boats
SET scores = list_filter(scores, score -> score <> $1)
WHERE id = $2;
