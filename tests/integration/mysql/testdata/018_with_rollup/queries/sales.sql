-- sgl.query(name: RevenueRollup, command: many)
SELECT region, city, GROUPING(region, city) AS level, SUM(amount) AS revenue, COUNT(*) AS sales
FROM sales
GROUP BY region, city WITH ROLLUP;

-- sgl.query(name: MonthlyRollup, command: many)
SELECT DATE_FORMAT(sold_on, '%Y-%m') AS month, SUM(amount) AS revenue
FROM sales
GROUP BY month WITH ROLLUP;

-- sgl.query(name: PositionalRollup, command: many)
SELECT UPPER(region) AS area, COUNT(*) AS sales
FROM sales
GROUP BY 1 WITH ROLLUP;
