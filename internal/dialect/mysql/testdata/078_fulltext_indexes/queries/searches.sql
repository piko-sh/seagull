-- sgl.query(name: Search1, command: many)
SELECT id FROM posts WHERE MATCH (title) AGAINST (?);

-- sgl.query(name: Search2, command: many)
SELECT id FROM notes WHERE MATCH (txt) AGAINST (?);

-- sgl.query(name: Search3, command: many)
SELECT id FROM posts WHERE MATCH (title, body) AGAINST (?);

-- sgl.query(name: Search4, command: many)
SELECT p.id FROM posts p WHERE MATCH (p.body, p.title) AGAINST (?);

-- sgl.query(name: Search5, command: many)
SELECT id FROM posts WHERE MATCH (body, title, title) AGAINST (?) OR MATCH (id) AGAINST (?);
