-- sgl.query(name: AddBook, command: exec)
INSERT INTO books (id, author_id, title, published)
VALUES (?, (SELECT id FROM authors WHERE name = ?), UPPER(?), DATE_ADD('2024-01-01', INTERVAL ? DAY));

-- sgl.query(name: DelayBook, command: exec)
UPDATE books SET published = DATE_ADD(published, INTERVAL ? HOUR) WHERE id = ?;

-- sgl.query(name: GetBook, command: one)
SELECT id, author_id, title, published FROM books WHERE id = ?;

-- sgl.query(name: BooksAfter, command: many)
SELECT id FROM books WHERE (published, id) > (?, ?) ORDER BY published, id;

-- sgl.query(name: BooksIn, command: many)
SELECT id FROM books WHERE (author_id, title) IN ((?, ?), (?, ?)) ORDER BY id;
