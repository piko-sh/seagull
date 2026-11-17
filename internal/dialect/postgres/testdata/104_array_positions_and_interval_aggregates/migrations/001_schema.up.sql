CREATE TABLE tasks (
    id integer PRIMARY KEY,
    project text NOT NULL,
    tags text[] NOT NULL,
    spent interval NOT NULL,
    estimate_hours integer NOT NULL
);
