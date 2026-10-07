-- sgl.query(name: BookCount, command: one)
SELECT count(*) AS total, (SELECT count(*) FROM authors WHERE id > 100) AS absent, array_agg(id ORDER BY id) AS ids FROM books;
