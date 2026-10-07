-- sgl.query(name: InsertArticle, command: exec)
INSERT INTO articles (title, body, author, published_at) VALUES (?, ?, ?, ?);

-- sgl.query(name: GetByAuthor, command: many)
SELECT id, title, author FROM articles WHERE author = ? ORDER BY id;

-- sgl.query(name: GetByTitle, command: one)
SELECT id, title, author FROM articles WHERE title = ?;

-- sgl.query(name: FulltextSearch, command: many)
SELECT id, title, author FROM articles WHERE MATCH(title, body) AGAINST(? IN BOOLEAN MODE) ORDER BY id;
