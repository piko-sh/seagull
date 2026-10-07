-- sgl.query(name: AddBook, command: exec)
INSERT INTO books (id, author_id, title, published)
VALUES (?, (SELECT id FROM authors WHERE name = ?), upper(?), date('2024-01-01', '+' || ? || ' days'));

-- sgl.query(name: GetBook, command: one)
SELECT id, author_id, title, published FROM books WHERE id = ?;
