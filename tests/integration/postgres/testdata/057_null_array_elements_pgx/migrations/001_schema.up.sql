CREATE TABLE readings (
    id integer PRIMARY KEY,
    label text NOT NULL,
    note text,
    scores integer[] NOT NULL
);
