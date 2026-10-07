CREATE TABLE readings (
    id       BIGINT  NOT NULL AUTO_INCREMENT PRIMARY KEY,
    taken_at CHAR(5) NOT NULL,
    level_cm INT     NOT NULL
);
