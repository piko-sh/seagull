CREATE SCHEMA billing;
CREATE TABLE billing.accounts (id bigint NOT NULL, CONSTRAINT accounts_key PRIMARY KEY (id));
CREATE TABLE owners (code bigint NOT NULL, email text, CONSTRAINT owners_key PRIMARY KEY (code), CONSTRAINT owners_email UNIQUE (email));
CREATE TABLE pets (id bigint NOT NULL, owner bigint, account bigint, age integer NOT NULL, weight integer DEFAULT 1,
    CONSTRAINT pets_key PRIMARY KEY (id),
    CONSTRAINT pets_owner FOREIGN KEY (owner) REFERENCES owners (code),
    CONSTRAINT pets_account FOREIGN KEY (account) REFERENCES billing.accounts (id));
ALTER TABLE owners RENAME COLUMN code TO owner_id;
ALTER TABLE owners RENAME COLUMN email TO contact;
ALTER TABLE owners RENAME TO keepers;
ALTER TABLE pets DROP CONSTRAINT pets_key;
ALTER TABLE pets ALTER COLUMN age SET DEFAULT 0;
ALTER TABLE pets ALTER COLUMN weight DROP DEFAULT;
