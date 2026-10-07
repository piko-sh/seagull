CREATE TABLE accounts (
    id      INTEGER PRIMARY KEY,
    owner   TEXT    NOT NULL,
    balance INTEGER NOT NULL CHECK (balance >= 0)
);
