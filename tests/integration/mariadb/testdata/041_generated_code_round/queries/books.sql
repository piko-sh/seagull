-- sgl.query(name: UpsertBooks, command: batch)
INSERT INTO books (id, title) VALUES (?, ?) ON DUPLICATE KEY UPDATE title = VALUES(title);

-- sgl.query(name: ListBooks, command: many)
SELECT b.* FROM books b ORDER BY b.id;

-- sgl.query(name: DeleteBook, command: one)
DELETE FROM books WHERE id = ? RETURNING *;
