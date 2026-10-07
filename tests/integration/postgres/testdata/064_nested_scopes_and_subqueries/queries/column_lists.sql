-- sgl.query(name: RenamedDerivedTable, command: many)
SELECT r.book, r.writer FROM (SELECT id, author_id FROM books) AS r (book, writer) WHERE r.writer = $1 ORDER BY r.book;

-- sgl.query(name: PartlyRenamedCTE, command: many)
WITH t (book) AS (SELECT id, title FROM books) SELECT book, title FROM t ORDER BY book;

-- sgl.query(name: RenamedStarCTE, command: many)
WITH t (key, label) AS (SELECT * FROM authors) SELECT key, label FROM t ORDER BY key;

-- sgl.query(name: RenamedValues, command: many)
SELECT v.n, v.label FROM (VALUES (1, 'one'), (2, 'two')) AS v (n, label) ORDER BY v.n;

-- sgl.query(name: AliasedJoin, command: many)
SELECT r.* FROM (authors a JOIN books b ON b.author_id = a.id) AS r ORDER BY 3;

-- sgl.query(name: AliasedJoinColumns, command: many)
SELECT r.writer, r.title FROM (authors a JOIN books b ON b.author_id = a.id AND b.title <> $1) AS r (author, writer, book, author_ref, title, price) ORDER BY r.title;
