-- sgl.query(name: AddBook, command: exec)
INSERT INTO books (id, author_id, title, published, tags)
VALUES ($1, (SELECT id FROM authors WHERE name = $2), upper($3), timestamp '2024-01-01' + $4 * interval '1 day', $5);

-- sgl.query(name: RetagBook, command: exec)
UPDATE books SET tags[$1] = $2 WHERE id = $3;

-- sgl.query(name: GetBook, command: one)
SELECT id, author_id, title, published, tags FROM books WHERE id = $1;

-- sgl.query(name: BooksAfter, command: many)
SELECT id FROM books WHERE (published, id) > ($1, $2) ORDER BY published, id;

-- sgl.query(name: HasTag, command: one)
SELECT 'crime' = ANY($1::text[]) AS tagged;
