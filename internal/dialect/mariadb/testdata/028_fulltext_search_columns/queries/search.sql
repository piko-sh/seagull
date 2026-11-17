-- sgl.query(name: SearchArchive, command: many)
SELECT id FROM archived_posts WHERE MATCH (body, title) AGAINST (?);

-- sgl.query(name: SearchBoth, command: many)
SELECT p.id FROM posts p JOIN notes n ON n.id = p.id WHERE MATCH (p.title, n.body) AGAINST (?);

-- sgl.query(name: SearchTitles, command: many)
SELECT id FROM posts WHERE MATCH (title) AGAINST (? IN BOOLEAN MODE);

-- sgl.query(name: SearchSummaries, command: many)
SELECT p.id FROM posts p WHERE MATCH (p.summary) AGAINST (?);
