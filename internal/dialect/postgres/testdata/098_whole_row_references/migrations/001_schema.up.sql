CREATE TABLE harbours (
    id integer PRIMARY KEY,
    name text NOT NULL,
    tags text[]
);

CREATE TABLE berths (
    id integer PRIMARY KEY,
    harbour_id integer NOT NULL REFERENCES harbours (id),
    places integer NOT NULL,
    plan jsonb
);

CREATE TABLE moorings (
    moorings integer NOT NULL
);

CREATE VIEW harbour_names AS SELECT id, name FROM harbours;
