CREATE TABLE sailings (
    id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    origin      VARCHAR(100) NOT NULL,
    destination VARCHAR(100) NOT NULL,
    departs     CHAR(5)      NOT NULL,
    seats_free  INT          NOT NULL
);
