-- sgl.query(NestedRight, many)
SELECT a.x, b.y, c.z FROM a LEFT JOIN (b RIGHT JOIN c ON c.b_id = b.id) ON b.a_id = a.id ORDER BY a.x, c.z;

-- sgl.query(NestedFull, many)
SELECT a.x, b.y, c.z FROM a LEFT JOIN (b FULL JOIN c ON c.b_id = b.id) ON b.a_id = a.id ORDER BY a.x, b.y, c.z;

-- sgl.query(NestedDerived, many)
SELECT a.x, d.y FROM a LEFT JOIN (b JOIN (SELECT id, y FROM b) d ON d.id = b.id) ON b.a_id = a.id ORDER BY a.x;

-- sgl.query(CountOffset, one)
SELECT (SELECT count(*) FROM c OFFSET 5) AS n;

-- sgl.query(CountLimitZero, one)
SELECT (SELECT count(*) FROM c LIMIT 0) AS n;

-- sgl.query(CountFetchZero, one)
SELECT (SELECT count(*) FROM c FETCH FIRST 0 ROWS ONLY) AS n;

-- sgl.query(CountPlain, one)
SELECT (SELECT count(*) FROM c) AS n;
