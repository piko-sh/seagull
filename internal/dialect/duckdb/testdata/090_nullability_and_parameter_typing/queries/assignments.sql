-- sgl.query(name: AddBook, command: exec)
INSERT INTO books (id, author_id, title, published, labels)
VALUES ($1, (SELECT id FROM authors WHERE name = $2), upper($3), now() + to_days($4), $5);

-- sgl.query(name: RepriceBook, command: exec)
UPDATE books SET price = price * $1, title = CASE WHEN $2 = 1 THEN NULLIF($3, '') ELSE title END WHERE id = $4;

-- sgl.query(name: AddReview, command: exec)
INSERT INTO books (id, author_id, title, published, labels) VALUES ($1, COALESCE((SELECT id FROM authors WHERE nickname = $2), 0), $3, now(), $4);

-- sgl.query(name: TouchBook, command: exec)
UPDATE books SET published = greatest(published, $1), price = least(price, $2), author_id = ifnull(author_id, $3) WHERE id = $4;
