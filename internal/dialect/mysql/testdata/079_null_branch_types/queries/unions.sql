-- sgl.query(NullSecond, many)
SELECT id, score FROM scores UNION ALL SELECT id, NULL FROM peers;

-- sgl.query(NullFirst, many)
SELECT id, NULL AS score FROM peers UNION ALL SELECT id, score FROM scores;

-- sgl.query(CaseElseNull, many)
SELECT CASE WHEN score > 0 THEN score ELSE NULL END AS positive FROM scores;
