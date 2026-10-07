-- sgl.query(name: AddBook, command: exec)
INSERT INTO books (id, author_id, title, published)
VALUES (?, (SELECT id FROM authors WHERE name = ?), upper(?), datetime('now', '+' || ? || ' days'));

-- sgl.query(name: RenameBook, command: exec)
UPDATE books SET title = title || ?, price = length(?) WHERE id = ?;

-- sgl.query(name: RepriceBook, command: exec)
UPDATE books SET price = price * ?, title = CASE WHEN ? = 1 THEN NULLIF(?, '') ELSE title END WHERE id = ?;

-- sgl.query(name: AddReview, command: exec)
INSERT INTO books (id, author_id, title, published) VALUES (?, COALESCE((SELECT id FROM authors WHERE nickname = ?), 0), ?, datetime('now'));

-- sgl.query(name: TouchBook, command: exec)
UPDATE books SET published = max(published, ?), price = min(price, ?), author_id = ifnull(author_id, ?) WHERE id = ?;
