-- sgl.query(name: OrderedSubtotals, command: many)
SELECT category, count(*) AS total FROM quotes GROUP BY category WITH ROLLUP ORDER BY category;

-- sgl.query(name: Subtotals, command: many)
SELECT category, count(*) AS total FROM quotes GROUP BY category WITH ROLLUP;
