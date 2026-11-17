-- sgl.query(ValueWindows, many)
SELECT
    id,
    LAG(id) OVER (ORDER BY id) AS previous_id,
    LEAD(id) OVER (ORDER BY id) AS next_id,
    FIRST_VALUE(score) OVER (ORDER BY id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS first_score,
    LAST_VALUE(score) OVER (ORDER BY id) AS last_score,
    NTH_VALUE(id, 2) OVER (ORDER BY id) AS second_id
FROM items;
