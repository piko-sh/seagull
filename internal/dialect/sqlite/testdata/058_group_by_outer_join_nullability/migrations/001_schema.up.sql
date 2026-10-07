CREATE TABLE harbours (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE boats (
    id INTEGER PRIMARY KEY,
    harbour_id INTEGER NOT NULL REFERENCES harbours (id),
    crew INTEGER NOT NULL
);
