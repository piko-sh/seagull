-- sgl.query(name: UpsertBooks, command: batch)
INSERT INTO books (id, title) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET title = excluded.title;

-- sgl.query(name: ListBooks, command: many)
SELECT b.* FROM books b ORDER BY b.id;

-- sgl.query(name: RetitleBook, command: one)
UPDATE books SET title = $2 WHERE id = $1 RETURNING *;
