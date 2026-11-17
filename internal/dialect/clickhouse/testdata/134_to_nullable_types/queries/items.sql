-- sgl.query(NullableColumns, many)
SELECT toNullable(id) AS id, toNullable(score) AS score FROM items;

-- sgl.query(RecursiveChain, many)
WITH RECURSIVE chain (n, a, b) AS (
    SELECT 1, toNullable(i.id), toNullable(i.category) FROM items i WHERE i.id <= 1
    UNION ALL
    SELECT n + 1, b, NULL FROM chain WHERE n < 4
)
SELECT n AS step, a AS value_a, b AS value_b FROM chain;
