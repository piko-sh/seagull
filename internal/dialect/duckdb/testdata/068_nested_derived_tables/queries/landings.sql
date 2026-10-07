-- sgl.query(PivotedLandings, many)
SELECT * FROM (SELECT harbour, species, kg FROM landings) AS l PIVOT (sum(kg) FOR species IN ('cod', 'hake'));

-- sgl.query(NestedKilos, many)
SELECT x FROM (SELECT x FROM (SELECT kg AS x FROM landings) a) b;
