CREATE TABLE posts (id INT PRIMARY KEY, title VARCHAR(200), body TEXT, FULLTEXT KEY posts_text (title, body));
CREATE TABLE notes (id INT PRIMARY KEY, body TEXT, FULLTEXT (body));
CREATE TABLE archived_posts LIKE posts;
