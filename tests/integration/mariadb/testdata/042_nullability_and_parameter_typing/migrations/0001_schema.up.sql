CREATE TABLE authors (id int PRIMARY KEY, name varchar(50) NOT NULL);
CREATE TABLE books (id int PRIMARY KEY, author_id int, title varchar(100) NOT NULL, price int, published datetime NOT NULL);
