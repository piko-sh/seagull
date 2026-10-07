-- sgl.query(name: AddBook, command: exec)
INSERT INTO books (id, author_id, title, published, tags)
VALUES ($1, (SELECT id FROM authors WHERE name = $2), upper($3), now() + $4 * interval '1 day', $5);

-- sgl.query(name: RetagBook, command: exec)
UPDATE books SET tags[$1] = $2, published = published + $3 * interval '1 hour' WHERE id = $4;

-- sgl.query(name: BooksAfter, command: many)
SELECT id, title FROM books WHERE (published, id) > ($1, $2) ORDER BY published, id;

-- sgl.query(name: HasTag, command: one)
SELECT 'x' = ANY($1::text[]) AS tagged;

-- sgl.query(name: RepriceBook, command: exec)
UPDATE books SET price = price * $1, title = title || $2, author_id = CASE WHEN $3::boolean THEN NULLIF($4, 0) ELSE author_id END WHERE id = $5;

-- sgl.query(name: AddReview, command: exec)
INSERT INTO books (id, author_id, title, published, tags)
VALUES ($1, COALESCE((SELECT id FROM authors WHERE nickname = $2), 0), $3, now(), $4);

-- sgl.query(name: TouchBook, command: exec)
UPDATE books SET published = GREATEST(published, $1), price = LEAST(price, $2) WHERE id = $3;
