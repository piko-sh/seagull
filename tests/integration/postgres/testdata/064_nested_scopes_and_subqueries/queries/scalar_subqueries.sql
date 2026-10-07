-- sgl.query(name: AuthorsWithFirstTitle, command: many)
SELECT a.id, (SELECT b.title FROM books b WHERE b.author_id = a.id ORDER BY b.id LIMIT 1), (SELECT max(b.price) FROM books b WHERE b.author_id = a.id) AS top_price
FROM authors a ORDER BY a.id;

-- sgl.query(name: AuthorsAboveCount, command: many)
SELECT a.id FROM authors a WHERE (SELECT count(*) FROM books b WHERE b.author_id = a.id) > $1;

-- sgl.query(name: NullInTheFirstBranch, command: many)
SELECT NULL AS name UNION ALL SELECT name FROM authors ORDER BY name NULLS FIRST;
