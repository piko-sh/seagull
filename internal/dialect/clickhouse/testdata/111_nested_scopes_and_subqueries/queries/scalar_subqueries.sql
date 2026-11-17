-- sgl.query(name: FirstTitle, command: many)
SELECT a.id, (SELECT b.title FROM books b ORDER BY b.id LIMIT 1), (SELECT max(b.price) FROM books b) AS top_price FROM authors a;

-- sgl.query(name: AuthorsBelowTheLargest, command: many)
SELECT a.id FROM authors a WHERE (SELECT max(b.author_id) FROM books b) > {largest:Int32};

-- sgl.query(name: NullInTheFirstBranch, command: many)
SELECT NULL AS name UNION ALL SELECT name FROM authors;
