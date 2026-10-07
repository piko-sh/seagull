-- sgl.query(BooksByAuthor, many)
SELECT id, author_id, pages, copies, rowid, COUNT(*) OVER () AS total
FROM books
WHERE author_id = ?
LIMIT ?;

-- sgl.query(BookTitles, many)
SELECT doc ->> '$.title' AS title,
       doc -> '$.tags' AS tags
FROM books
WHERE doc ->> '$.title' = ?;
