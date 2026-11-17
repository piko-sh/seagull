-- sgl.query(name: UpsertBooks, command: batch)
INSERT INTO books (id, title) VALUES (?, ?) ON DUPLICATE KEY UPDATE title = VALUES(title);

-- sgl.query(name: ListBooks, command: many)
SELECT b.* FROM books b ORDER BY b.id;
