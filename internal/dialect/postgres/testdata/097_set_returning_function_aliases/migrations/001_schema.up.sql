CREATE TABLE articles (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    tags TEXT[] NOT NULL,
    first_tag INTEGER NOT NULL,
    last_tag INTEGER NOT NULL
);

CREATE FUNCTION article_ids() RETURNS SETOF INTEGER LANGUAGE sql AS 'SELECT id FROM articles';
