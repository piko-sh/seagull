-- sgl.query(Predicates, many)
SELECT isNull(note) AS missing, isNotNull(note) AS present, isZeroOrNull(score) AS zero_or_missing,
       empty(name) AS unnamed, has(tags, 'gull') AS tagged, like(name, 'g%') AS gull_like,
       startsWith(name, 'g') AS starts, mapContains(ringings, 'a') AS ringed,
       hasTokenOrNull(name, 'gull') AS has_token, isNaN(toFloat64(score)) AS not_a_number
FROM gulls;

-- sgl.query(LogicalResults, many)
SELECT and(score, 1) AS both_set, or(active, score) AS either, xor(score, 1) AS one_of, not(score) AS negated,
       NOT active AS inactive, NOT score AS zero, toBool(score) AS flag
FROM gulls;
