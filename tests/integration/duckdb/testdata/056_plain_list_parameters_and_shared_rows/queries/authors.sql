-- sgl.query(AddAuthor, exec)
INSERT INTO authors (id, name, bio) VALUES ($1, $2, $3);

-- sgl.query(ListAuthors, many, row: AuthorSummary)
SELECT id, name, bio FROM authors ORDER BY id;

-- sgl.query(AuthorNamed, one, row: AuthorSummary)
SELECT bio, name, id FROM authors WHERE name = $1;
