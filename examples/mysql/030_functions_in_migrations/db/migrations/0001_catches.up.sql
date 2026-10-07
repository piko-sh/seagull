CREATE TABLE catches (
    id           BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    boat         VARCHAR(50) NOT NULL,
    species      VARCHAR(50) NOT NULL,
    weight_grams INT         NOT NULL
);

CREATE TABLE prices (
    species        VARCHAR(50) NOT NULL PRIMARY KEY,
    pence_per_kilo INT         NOT NULL
);
