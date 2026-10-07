CREATE TABLE accounts (
    id      BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    owner   VARCHAR(50) NOT NULL,
    balance INT         NOT NULL,
    CONSTRAINT balance_not_negative CHECK (balance >= 0)
);
