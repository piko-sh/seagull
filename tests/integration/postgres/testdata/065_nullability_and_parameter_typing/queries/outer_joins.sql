-- sgl.query(name: AuthorsOfBooks, command: many)
WITH b AS (SELECT id, author_id FROM books)
SELECT a.id AS author_id, a.name, b.id AS book_id FROM authors a RIGHT JOIN b ON b.author_id = a.id AND a.id < 2 ORDER BY b.id;

-- sgl.query(name: AuthorsBesideBooks, command: many)
SELECT d.id AS author_id, b.title FROM (SELECT id FROM authors) d FULL JOIN books b ON b.author_id = d.id ORDER BY d.id, b.id;
