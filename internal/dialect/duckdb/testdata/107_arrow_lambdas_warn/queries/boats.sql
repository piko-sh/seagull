-- sgl.query(ArrowLambdas, many)
SELECT list_filter(scores, score -> score > $1) AS high,
       list_reduce(scores, (total, score) -> total + score) AS total
FROM boats;

-- sgl.query(KeywordLambdasAndJSONArrows, many)
SELECT list_filter(scores, lambda score: score > $1) AS high,
       log -> '$.harbour' AS harbour,
       log ->> '$.skipper' AS skipper
FROM boats;
