-- sgl.query(name: TagsByTitle, command: many)
SELECT title, tag
FROM articles, unnest(tags) AS tag
WHERE tag <> :excluded
ORDER BY title, tag;

-- sgl.query(name: TagSlice, command: many)
SELECT id, tags[first_tag:last_tag] AS chosen
FROM articles
WHERE id = :id;

-- sgl.query(name: Numbers, command: many)
SELECT n, n.n AS qualified
FROM generate_series(1, 3) AS n;

-- sgl.query(name: ArticleIds, command: many)
SELECT article_id
FROM article_ids() AS article_id;
