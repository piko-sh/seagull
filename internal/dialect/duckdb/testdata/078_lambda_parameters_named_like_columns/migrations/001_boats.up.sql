CREATE TABLE boats (
    id INTEGER NOT NULL,
    score VARCHAR,
    tag INTEGER,
    scores INTEGER[] NOT NULL,
    crew STRUCT(name VARCHAR, age INTEGER)[] NOT NULL
);
