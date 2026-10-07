CREATE TABLE authors (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE books (id integer PRIMARY KEY, author_id integer REFERENCES authors (id), title text NOT NULL, price integer,
    tags text[] NOT NULL, published timestamp NOT NULL);
