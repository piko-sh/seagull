-- sgl.query(ReadingFlags, many)
SELECT flag, (flag) AS wrapped, CAST(amount AS BOOLEAN) AS cast_amount, COALESCE(CAST(amount AS BOOLEAN), flag) AS mixed
FROM readings;
