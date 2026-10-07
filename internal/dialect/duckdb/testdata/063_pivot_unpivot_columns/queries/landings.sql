-- sgl.query(name: Pivoted, command: many)
PIVOT landings ON species IN ('cod', 'hake') USING sum(kg) GROUP BY harbour;

-- sgl.query(name: PivotTwoAggregates, command: many)
PIVOT landings ON species IN ('cod', 'hake') USING sum(kg) AS total, count(*) AS catches GROUP BY harbour ORDER BY harbour;

-- sgl.query(name: PivotImplicitGroup, command: many)
PIVOT landings ON species IN ('cod' AS c, 'hake' AS h) USING max(kg);

-- sgl.query(name: PivotSubquery, command: many)
SELECT harbour, cod, hake FROM (PIVOT landings ON species IN ('cod', 'hake') USING sum(kg) GROUP BY harbour) WHERE cod > $1;

-- sgl.query(name: StandardPivot, command: many)
SELECT * FROM landings PIVOT (sum(kg) FOR species IN ('cod', 'hake') GROUP BY harbour);

-- sgl.query(name: Unpivoted, command: many)
UNPIVOT (SELECT harbour, kg, price FROM landings WHERE kg > $1) ON kg, price INTO NAME measure VALUE amount;

-- sgl.query(name: StandardUnpivot, command: many)
SELECT * FROM landings UNPIVOT INCLUDE NULLS (amount FOR measure IN (kg, price)) WHERE harbour = $1;
