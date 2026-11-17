-- sgl.query(name: AddSale, command: exec)
INSERT INTO sales (category) VALUES (?);

-- sgl.query(name: RolledUp, command: many)
SELECT category, count(*) AS total FROM sales GROUP BY ROLLUP (category);

-- sgl.query(name: WithRollupTotals, command: many)
SELECT category, count(*) AS total FROM sales GROUP BY category WITH ROLLUP;
