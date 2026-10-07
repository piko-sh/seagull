CREATE TABLE probes (
    id INTEGER PRIMARY KEY,
    label TEXT NOT NULL,
    amount INTEGER NOT NULL,
    ratio REAL NOT NULL,
    flag BOOLEAN NOT NULL,
    note TEXT NOT NULL,
    payload BLOB NOT NULL,
    optional_label TEXT,
    optional_amount INTEGER
);
