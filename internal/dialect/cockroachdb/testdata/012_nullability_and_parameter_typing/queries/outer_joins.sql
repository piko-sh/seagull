-- sgl.query(name: AuthorsOfBooks, command: many)
WITH a AS (SELECT id, name FROM authors)
SELECT a.id AS author_id, a.name, b.id AS book_id FROM a RIGHT JOIN books b ON b.author_id = a.id;

-- sgl.query(name: AuthorsBesideBooks, command: many)
SELECT d.id AS author_id, b.title FROM (SELECT id FROM authors) d FULL JOIN books b ON b.author_id = d.id;
