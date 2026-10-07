CREATE SEQUENCE account_ids;

CREATE TABLE accounts (
    id      BIGINT  PRIMARY KEY DEFAULT nextval('account_ids'),
    owner   VARCHAR NOT NULL,
    balance INTEGER NOT NULL CHECK (balance >= 0)
);
