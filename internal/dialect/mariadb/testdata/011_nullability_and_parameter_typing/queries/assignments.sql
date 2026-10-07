-- sgl.query(name: AddBook, command: exec)
INSERT INTO books (id, author_id, title, published)
VALUES (?, (SELECT id FROM authors WHERE name = ?), UPPER(?), DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? DAY));

-- sgl.query(name: RenameBook, command: exec)
UPDATE books SET title = CONCAT(title, ?), published = DATE_ADD(published, INTERVAL ? HOUR) WHERE id = ?;

-- sgl.query(name: BooksAfter, command: many)
SELECT id, title FROM books WHERE (published, id) > (?, ?) ORDER BY published, id;

-- sgl.query(name: BooksIn, command: many)
SELECT id FROM books WHERE (author_id, title) IN ((?, ?), (?, ?));

-- sgl.query(name: RepriceBook, command: exec)
UPDATE books SET price = price * ?, title = CASE WHEN ? = 1 THEN NULLIF(?, '') ELSE title END WHERE id = ?;

-- sgl.query(name: AddReview, command: exec)
INSERT INTO books (id, author_id, title, published) VALUES (?, COALESCE((SELECT id FROM authors WHERE nickname = ?), 0), ?, UTC_TIMESTAMP());

-- sgl.query(name: TouchBook, command: exec)
UPDATE books SET published = GREATEST(published, ?), price = LEAST(price, ?), author_id = IFNULL(author_id, ?) WHERE id = ?;
