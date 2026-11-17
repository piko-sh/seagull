CREATE TABLE owners (code INT8 NOT NULL, email STRING, CONSTRAINT owners_key PRIMARY KEY (code), CONSTRAINT owners_email UNIQUE (email));
CREATE TABLE pets (id INT8 NOT NULL PRIMARY KEY, owner INT8, CONSTRAINT pets_owner FOREIGN KEY (owner) REFERENCES owners (code));
ALTER TABLE owners RENAME COLUMN code TO owner_id;
ALTER TABLE owners RENAME COLUMN email TO contact;
ALTER TABLE owners RENAME TO keepers;
