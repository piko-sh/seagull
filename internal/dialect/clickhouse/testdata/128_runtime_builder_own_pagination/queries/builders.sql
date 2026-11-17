-- sgl.query(name: TopPerGroup, command: many, dynamic: runtime)
SELECT id, grp, score FROM items ORDER BY score DESC LIMIT 2 BY grp;

-- sgl.query(name: TopItems, command: many, dynamic: runtime)
SELECT id, grp, score FROM items WHERE score > {min:Int64} ORDER BY score DESC LIMIT {lim:UInt64};
