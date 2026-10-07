-- sgl.query(name: BookCount, command: one)
SELECT count(*) AS total, (SELECT count(*) FROM authors WHERE id > 100) AS absent FROM books;

-- sgl.query(name: UngroupedTitle, command: one)
SELECT title, count(*) AS total FROM books WHERE id > 100;
