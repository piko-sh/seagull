CREATE TABLE documents (
    id INTEGER PRIMARY KEY,
    title VARCHAR NOT NULL,
    tags VARCHAR[] NOT NULL,
    scores INTEGER[]
);
