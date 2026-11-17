-- sgl.query(name: TopItems, command: many, dynamic: runtime)
SELECT id, grp, score FROM items WHERE score IS NOT NULL ORDER BY score DESC LIMIT 10;

-- sgl.query(name: ItemPage, command: many, dynamic: runtime)
SELECT id, grp FROM items ORDER BY id LIMIT $1 OFFSET $2;

-- sgl.query(name: LaterItems, command: many, dynamic: runtime)
SELECT id, grp FROM items ORDER BY id OFFSET 5 ROWS FETCH FIRST 10 ROWS ONLY;

-- sgl.query(name: BestPerGroup, command: many, dynamic: runtime)
SELECT DISTINCT ON (grp) id, grp, score FROM items WHERE score IS NOT NULL ORDER BY grp, score DESC;
