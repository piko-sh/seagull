CREATE TABLE readings (
    id INTEGER PRIMARY KEY,
    amount INTEGER NOT NULL,
    label TEXT NOT NULL
);
INSERT INTO readings VALUES (1, 5, 'gull'), (2, -3, 'tern');
