-- sgl.query(name: TopScores, command: many, dynamic: runtime)
SELECT id, grp, score FROM items ORDER BY score DESC, id LIMIT 3;

-- sgl.query(name: BestPerGroup, command: many, dynamic: runtime)
SELECT DISTINCT ON (grp) id, grp, score FROM items ORDER BY grp, score DESC;
