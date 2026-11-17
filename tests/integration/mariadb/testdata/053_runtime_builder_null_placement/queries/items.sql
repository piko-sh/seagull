-- sgl.query(name: ListItems, command: many, dynamic: runtime)
SELECT id, score FROM items;

-- sgl.query(name: ListScores, command: many, dynamic: runtime)
SELECT DISTINCT score FROM items;
