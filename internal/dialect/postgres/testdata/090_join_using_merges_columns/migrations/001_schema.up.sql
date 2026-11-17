CREATE TABLE harbours (
    name text NOT NULL,
    harbour_id integer PRIMARY KEY,
    region text NOT NULL
);

CREATE TABLE moorings (
    berth text NOT NULL,
    harbour_id integer NOT NULL REFERENCES harbours (harbour_id),
    region text NOT NULL
);
