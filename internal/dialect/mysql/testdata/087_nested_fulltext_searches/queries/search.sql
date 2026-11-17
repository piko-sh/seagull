-- sgl.query(name: IndexedInSubquery, command: many)
SELECT id FROM notes WHERE post_id IN (SELECT id FROM posts WHERE MATCH (title) AGAINST (?));

-- sgl.query(name: UnindexedInSubquery, command: many)
SELECT id FROM posts WHERE EXISTS (SELECT 1 FROM notes AS n WHERE n.post_id = posts.id AND MATCH (n.txt) AGAINST (?));

-- sgl.query(name: UnindexedInScalarSubquery, command: many)
SELECT id, (SELECT count(*) FROM posts AS p WHERE MATCH (body) AGAINST (?)) AS hits FROM notes;

-- sgl.query(name: UnknownColumn, command: many)
SELECT id FROM posts WHERE MATCH (summary) AGAINST (?);
