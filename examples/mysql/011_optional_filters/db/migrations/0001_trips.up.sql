CREATE TABLE trips (
    id           BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name         VARCHAR(50) NOT NULL,
    destination  VARCHAR(50) NOT NULL,
    price_pence  INT         NOT NULL,
    dogs_welcome BOOLEAN     NOT NULL
);
