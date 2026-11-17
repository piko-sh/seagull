-- sgl.query(name: RevenueRollup, command: many)
SELECT region, city, grouping(region, city) AS level, sum(amount) AS revenue
FROM sales
GROUP BY ROLLUP(region, city)
ORDER BY level, region, city;

-- sgl.query(name: RevenueCube, command: many)
SELECT region, city, sum(amount) AS revenue
FROM sales
GROUP BY region, city WITH CUBE
ORDER BY region, city;

-- sgl.query(name: RevenueSets, command: many)
SELECT region, city, sum(amount) AS revenue
FROM sales
GROUP BY GROUPING SETS ((region, city), (region), ())
ORDER BY region, city;

-- sgl.query(name: RevenueRollupWithNulls, command: many)
SELECT region, sum(amount) AS revenue
FROM sales
GROUP BY region WITH ROLLUP
ORDER BY region
SETTINGS group_by_use_nulls = 1;
