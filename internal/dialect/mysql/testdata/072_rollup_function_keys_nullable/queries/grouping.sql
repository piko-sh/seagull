-- sgl.query(name: Rollup, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY ROLLUP (k);

-- sgl.query(name: WithRollup, command: many)
SELECT k, count(*) AS n FROM rt GROUP BY k WITH ROLLUP;
