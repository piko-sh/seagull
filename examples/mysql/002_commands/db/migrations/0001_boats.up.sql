CREATE TABLE boats (
    id     BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name   VARCHAR(100) NOT NULL,
    berth  INT          NOT NULL,
    at_sea BOOLEAN      NOT NULL DEFAULT FALSE
);
