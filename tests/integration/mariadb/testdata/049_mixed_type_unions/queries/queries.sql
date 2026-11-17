-- sgl.query(MixedColumns, many)
SELECT count AS small, total AS wide, delta AS number_or_text FROM readings
UNION ALL
SELECT delta, offset_value, label FROM readings
ORDER BY 1, 2, 3;

-- sgl.query(MixedBranches, many)
SELECT IF(id > 1, count, delta) AS small, IF(id > 1, total, offset_value) AS wide, GREATEST(count, delta) AS biggest
FROM readings
ORDER BY id;

-- sgl.query(Aggregates, one)
SELECT AVG(price) AS average_price, AVG(delta) AS average_delta, SUM(weight) AS total_weight FROM readings;
