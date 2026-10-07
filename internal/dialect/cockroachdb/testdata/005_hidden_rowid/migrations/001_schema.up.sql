CREATE TABLE events (
    name STRING NOT NULL,
    happened_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE accounts (
    id INT PRIMARY KEY,
    email STRING NOT NULL
);

CREATE TABLE audit (
    account_id INT NOT NULL,
    note STRING,
    CONSTRAINT audit_pk PRIMARY KEY (account_id)
);
