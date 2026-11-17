-- sgl.query(MixedColumns, many)
SELECT amount AS number_or_text FROM readings
UNION ALL
SELECT label FROM readings
ORDER BY 1;
