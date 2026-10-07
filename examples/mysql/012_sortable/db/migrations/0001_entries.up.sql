CREATE TABLE entries (
    id             BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    boat           VARCHAR(50) NOT NULL,
    crew           INT         NOT NULL,
    finish_seconds INT         NOT NULL
);
