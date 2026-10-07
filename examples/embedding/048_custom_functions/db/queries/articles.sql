-- sgl.query(CreateArticle, one)
INSERT INTO articles (title) VALUES (?) RETURNING id, title;

-- sgl.query(ListArticleSlugs, many)
SELECT id, title, slugify(title) AS slug FROM articles ORDER BY id;

-- sgl.query(FindArticleBySlug, one)
-- :slug as sgl.param(slug, type: text)
SELECT id, title FROM articles WHERE slugify(title) = :slug;
