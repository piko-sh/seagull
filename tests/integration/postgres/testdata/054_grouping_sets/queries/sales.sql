-- sgl.query(name: RevenueRollup, command: many)
SELECT region, city, upper(city) AS loud_city, coalesce(city, 'all cities') AS city_label,
       grouping(region, city) AS level, sum(amount) AS revenue, count(*) AS sales
FROM sales
GROUP BY ROLLUP (region, city)
ORDER BY 1 NULLS LAST, 2 NULLS LAST;

-- sgl.query(name: RegionCityRollup, command: many)
SELECT region, city, sum(amount) AS revenue
FROM sales
GROUP BY region, ROLLUP (city)
ORDER BY 1 NULLS LAST, 2 NULLS LAST;

-- sgl.query(name: ProductCube, command: many)
SELECT s.region, s.product, count(*) AS sales
FROM sales s
GROUP BY CUBE (s.region, s.product)
ORDER BY 1 NULLS LAST, 2 NULLS LAST;

-- sgl.query(name: GroupingSets, command: many)
SELECT region, product, date_trunc('month', sold_on) AS month, sum(amount) AS revenue
FROM sales
GROUP BY GROUPING SETS ((region, product), (date_trunc('month', sold_on)), ())
ORDER BY 1 NULLS LAST, 2 NULLS LAST, 3 NULLS LAST;

-- sgl.query(name: PositionalRollup, command: many)
SELECT lower(region) AS area, sum(amount) AS revenue
FROM sales
GROUP BY ROLLUP (1)
ORDER BY 1 NULLS LAST;

-- sgl.query(name: AliasRollup, command: many)
SELECT lower(region) AS area, sum(amount) AS revenue
FROM sales
GROUP BY ROLLUP (area)
ORDER BY 1 NULLS LAST;
