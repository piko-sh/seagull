-- sgl.query(name: RenamedDerivedTable, command: many)
SELECT r.book, r.writer FROM (SELECT id, author_id FROM books) AS r (book, writer) WHERE r.writer = ?;

-- sgl.query(name: RenamedStarCTE, command: many)
WITH t (`key`, label) AS (SELECT * FROM authors) SELECT `key`, label FROM t;

-- sgl.query(name: RenamedValues, command: many)
SELECT v.n, v.label FROM (VALUES ROW(1, 'one'), ROW(2, 'two')) AS v (n, label);
