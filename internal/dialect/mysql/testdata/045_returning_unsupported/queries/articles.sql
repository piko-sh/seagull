-- sgl.query(CreateArticle, one)
INSERT INTO articles (title) VALUES (?) RETURNING id, title;

-- sgl.query(RenameArticle, one)
UPDATE articles
SET title = ?
WHERE id = ?
RETURNING id, title;

-- sgl.query(DeleteArticle, one)
DELETE FROM articles WHERE id = ? RETURNING id;

-- sgl.query(GetArticle, one)
SELECT id, title FROM articles WHERE id = ?;
