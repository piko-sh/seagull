-- sgl.query(HighScores, many)
SELECT list_filter(scores, score -> score > $1) AS high
FROM boats;

-- sgl.query(TaggedBoats, many)
SELECT id
FROM boats
WHERE len(list_filter(tags, tag -> tag = $1)) > 0;

-- sgl.query(LaterScores, many)
SELECT list_filter(scores, (score, position) -> position > $1) AS later
FROM boats;

-- sgl.query(Positions, many)
SELECT list_transform(tags, (tag, position) -> position) AS positions,
       list_reduce(scores, (total, score) -> total + score) AS total,
       list_reduce(scores, (total, score, position) -> total + position) AS weighted,
       list_reduce(scores, (total, score) -> total + score, 0::BIGINT) AS wide_total,
       list_reduce(tags, (joined, tag) -> joined || tag) AS joined
FROM boats;
