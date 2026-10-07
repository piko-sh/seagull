-- sgl.query(name: BookCount, command: one)
SELECT count() AS total, (SELECT count() FROM authors) AS authors, groupArray(id) AS ids FROM books;
