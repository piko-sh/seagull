-- sgl.query(name: FilterAuthors, command: many)
SELECT id, name FROM authors WHERE name = COALESCE($1, name) AND email = COALESCE(:email, email);

-- sgl.query(name: RenameAuthor, command: exec)
UPDATE authors SET name = COALESCE($2, name), bio = COALESCE($3, bio) WHERE id = $1;

-- sgl.query(name: AuthorsByIDs, command: many)
SELECT id, name FROM authors WHERE id = ANY($1);

-- sgl.query(name: AuthorsOutside, command: many)
SELECT id, name FROM authors WHERE id <> ALL($1) AND lower(email) = lower($2);

-- sgl.query(name: BooksBetween, command: many)
SELECT id, title FROM books WHERE published BETWEEN $1 AND $2 AND $3 < id;

-- sgl.query(name: AuthorUnlessExcluded, command: many)
SELECT NULLIF(id, $1) AS kept FROM authors WHERE NULLIF($2, '') = email;

-- sgl.query(name: Flagged, command: many)
SELECT id, CASE WHEN $1 THEN name ELSE '' END AS shown FROM authors WHERE $2 AND NOT $3;

-- sgl.query(name: AddAuthor, command: exec)
INSERT INTO authors VALUES ($1, $2, $3);

-- sgl.query(name: Lock, command: exec)
SELECT pg_advisory_lock($1);
