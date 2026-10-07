-- sgl.query(name: BookCount, command: one)
SELECT count(*) AS total, (SELECT count(*) FROM authors) AS authors, array_agg(id) AS ids, array_agg(price) AS prices FROM books;
