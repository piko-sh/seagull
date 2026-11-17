-- sgl.query(MixedColumns, many)
SELECT count AS small, total AS wide, delta AS number_or_text FROM readings
UNION ALL
SELECT delta, offset_value, label FROM readings
ORDER BY 1, 2, 3;
