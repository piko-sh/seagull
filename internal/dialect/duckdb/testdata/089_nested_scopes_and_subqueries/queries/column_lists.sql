-- sgl.query(name: RenamedDerivedTable, command: many)
SELECT r.book, r.writer FROM (SELECT id, author_id FROM books) AS r (book, writer) WHERE r.writer = $1;

-- sgl.query(name: PartlyRenamedCTE, command: many)
WITH t (book) AS (SELECT id, title FROM books) SELECT book, title FROM t;

-- sgl.query(name: RenamedStarCTE, command: many)
WITH t (key, label) AS (SELECT * FROM authors) SELECT key, label FROM t;

-- sgl.query(name: RenamedValues, command: many)
SELECT v.n, v.label FROM (VALUES (1, 'one'), (2, 'two')) AS v (n, label);
