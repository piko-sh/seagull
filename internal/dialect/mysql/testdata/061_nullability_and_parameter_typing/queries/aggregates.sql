-- sgl.query(name: BookCount, command: one)
SELECT count(*) AS total, (SELECT count(*) FROM authors) AS authors FROM books;

-- sgl.query(name: UngroupedTitle, command: one)
SELECT title, count(*) AS total FROM books;
