-- sgl.query(name: Plain, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY k;

-- sgl.query(name: Rollup, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY ROLLUP (k);

-- sgl.query(name: WithRollup, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY k WITH ROLLUP;

-- sgl.query(name: Cube, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY CUBE (k);

-- sgl.query(name: Sets, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY GROUPING SETS ((k), ());
