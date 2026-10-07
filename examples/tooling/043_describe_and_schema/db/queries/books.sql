-- sgl.query(ListBooksByAuthor, many)
SELECT books.id, books.title, books.subtitle, books.published
FROM books
JOIN authors ON authors.id = books.author_id
WHERE authors.name = ?
ORDER BY books.published;

-- sgl.query(AddBook, exec)
INSERT INTO books (author_id, title, subtitle, published) VALUES (?, ?, ?, ?);
