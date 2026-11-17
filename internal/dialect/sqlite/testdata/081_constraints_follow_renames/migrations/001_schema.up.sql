CREATE TABLE owners (code INTEGER NOT NULL, email TEXT, CONSTRAINT owners_key PRIMARY KEY (code), CONSTRAINT owners_email UNIQUE (email));
CREATE TABLE pets (id INTEGER NOT NULL PRIMARY KEY, owner INTEGER, CONSTRAINT pets_owner FOREIGN KEY (owner) REFERENCES owners (code));
ALTER TABLE owners RENAME COLUMN code TO owner_id;
ALTER TABLE owners RENAME COLUMN email TO contact;
ALTER TABLE owners RENAME TO keepers;
