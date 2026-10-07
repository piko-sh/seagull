-- sgl.query(name: PostsByAuthor, command: many, dynamic: runtime)
-- ?1 as sgl.param(author)
SELECT id, author, title, views FROM posts WHERE author = ?1

-- sgl.query(name: PostsByAuthorNamed, command: many, dynamic: runtime)
SELECT id, author, title, views FROM posts WHERE author = :author AND views >= :min_views
