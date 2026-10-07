CREATE TABLE visits (
    id INTEGER PRIMARY KEY,
    visitor VARCHAR NOT NULL,
    visited DATE NOT NULL,
    stays INTEGER
);

CREATE MACRO year_of(day) AS year(day);

CREATE MACRO shout(word) AS upper(word) || '!';

CREATE MACRO doubled(amount) AS amount * 2;

CREATE MACRO kept(amount) AS coalesce(amount, 0);
