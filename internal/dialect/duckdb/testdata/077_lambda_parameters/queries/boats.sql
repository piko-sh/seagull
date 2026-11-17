-- sgl.query(HighScores, many)
SELECT list_filter(scores, lambda score: score > $1) AS high
FROM boats;

-- sgl.query(TaggedBoats, many)
SELECT id
FROM boats
WHERE len(list_filter(tags, lambda tag: tag = $1)) > 0;

-- sgl.query(LaterScores, many)
SELECT list_filter(scores, lambda score, position: position > $1) AS later
FROM boats;

-- sgl.query(Positions, many)
SELECT list_transform(tags, lambda tag, position: position) AS positions,
       list_reduce(scores, lambda total, score: total + score) AS total,
       list_reduce(scores, lambda total, score, position: total + position) AS weighted,
       list_reduce(scores, lambda total, score: total + score, 0::BIGINT) AS wide_total,
       list_reduce(tags, lambda joined, tag: joined || tag) AS joined
FROM boats;
