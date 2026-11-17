-- sgl.query(InsertMetric, exec)
INSERT INTO metrics (id, value) VALUES ({id:UInt64}, {value:UInt32});

-- sgl.query(FilterByValue, many)
SELECT id, value
FROM metrics
WHERE value BETWEEN {lower:UInt32} AND {upper:UInt32}
ORDER BY id;
