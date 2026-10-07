CREATE TABLE accounts (
    id      INT8    NOT NULL DEFAULT unique_rowid() PRIMARY KEY,
    owner   STRING  NOT NULL,
    balance INTEGER NOT NULL CHECK (balance >= 0)
);
