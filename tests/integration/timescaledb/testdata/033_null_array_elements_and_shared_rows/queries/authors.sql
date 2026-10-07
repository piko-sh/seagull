-- sgl.query(UpsertAuthors, exec)
-- :ids as sgl.param(non_null_elements: true)
-- :names as sgl.param(non_null_elements: true)
INSERT INTO authors (id, name, bio)
SELECT * FROM unnest(:ids::bigint[], :names::text[], :bios::text[])
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, bio = EXCLUDED.bio;

-- sgl.query(ListAuthors, many, row: AuthorSummary)
SELECT id, name, bio FROM authors ORDER BY id;

-- sgl.query(AuthorsNamed, many, row: AuthorSummary)
SELECT bio, name, id FROM authors WHERE name = ANY($1) ORDER BY id;
