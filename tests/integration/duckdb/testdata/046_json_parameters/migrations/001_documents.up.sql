CREATE TABLE documents (
    id INTEGER PRIMARY KEY,
    data JSON
);

CREATE TABLE composites (
    id INTEGER PRIMARY KEY,
    documents JSON[] NOT NULL,
    pair STRUCT(doc JSON, n INTEGER) NOT NULL
);
