CREATE TABLE people (
    id INT PRIMARY KEY,
    first_name VARCHAR(50) NOT NULL,
    last_name VARCHAR(50) NOT NULL,
    born VARCHAR(20) NOT NULL,
    signed_up DATETIME NOT NULL,
    visits INT NOT NULL,
    spend DECIMAL(10, 2) NOT NULL,
    fingerprint VARCHAR(16) CHARACTER SET binary,
    notes TEXT CHARSET binary NOT NULL
);

CREATE TABLE archived_people (id INT PRIMARY KEY, first_name VARCHAR(50) NOT NULL);

CREATE VIEW newest_and_oldest AS
    (SELECT id, first_name FROM people ORDER BY id DESC LIMIT 1)
    UNION ALL
    (SELECT id, first_name FROM people ORDER BY id LIMIT 1);
