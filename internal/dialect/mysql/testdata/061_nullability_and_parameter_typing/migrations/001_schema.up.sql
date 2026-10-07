CREATE TABLE authors (id int PRIMARY KEY, name varchar(50) NOT NULL, nickname varchar(50));
CREATE TABLE books (id int PRIMARY KEY, author_id int, title varchar(100) NOT NULL, price decimal(10, 2), published datetime NOT NULL);
