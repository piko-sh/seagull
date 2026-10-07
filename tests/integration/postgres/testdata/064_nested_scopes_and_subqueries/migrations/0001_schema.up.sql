CREATE TABLE authors (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE books (id integer PRIMARY KEY, author_id integer NOT NULL REFERENCES authors (id), title text NOT NULL, price integer);
