CREATE TABLE sessions (
    id BINARY(16) PRIMARY KEY,
    user_id INT NOT NULL,
    device VARCHAR(40) NOT NULL,
    ip VARCHAR(45) NOT NULL
);

CREATE TABLE sequences (
    name VARCHAR(40) PRIMARY KEY,
    value BIGINT NOT NULL
);
