CREATE TABLE authors (id integer PRIMARY KEY, name text NOT NULL, nickname text);
CREATE TABLE books (id integer PRIMARY KEY, author_id integer REFERENCES authors (id), title text NOT NULL, price numeric(10, 2),
    tags text[] NOT NULL, published timestamp NOT NULL);
