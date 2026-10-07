-- sgl.query(name: UpsertBooks, command: batch)
INSERT INTO books (id, author_id, title, labels) VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, labels = EXCLUDED.labels;

-- sgl.query(name: BooksWithAuthors, command: many)
-- sgl.embed(authors)
SELECT books.id, books.title, authors.* FROM books JOIN authors ON authors.id = books.author_id ORDER BY books.id;

-- sgl.query(name: RetitleBook, command: one)
UPDATE books SET title = $2 WHERE id = $1 RETURNING *;
