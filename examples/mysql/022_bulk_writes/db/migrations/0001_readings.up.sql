CREATE TABLE readings (
    id        BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    station   VARCHAR(20) NOT NULL,
    minute    INT         NOT NULL,
    height_cm INT         NOT NULL,
    note      VARCHAR(50)
);
