-- sgl.query(name: Plain, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY k;

-- sgl.query(name: WithRollup, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY k WITH ROLLUP;

-- sgl.query(name: Sets, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY GROUPING SETS ((k), ());
