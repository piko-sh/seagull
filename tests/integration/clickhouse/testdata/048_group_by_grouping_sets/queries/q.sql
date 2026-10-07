-- sgl.query(InsertOrder, exec)
INSERT INTO orders (id, category, day, amount) VALUES ({id:UInt64}, {category:String}, {day:Date}, {amount:UInt64});

-- sgl.query(GroupedTotals, many)
SELECT
    category,
    day,
    sum(amount) AS total
FROM orders
GROUP BY GROUPING SETS ((category, day), (category), ())
ORDER BY category, day, total;
