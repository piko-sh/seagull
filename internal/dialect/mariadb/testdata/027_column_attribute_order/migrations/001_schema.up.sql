CREATE TABLE owners (id INT PRIMARY KEY);
CREATE TABLE accepted (
    id INT NOT NULL DEFAULT 1 COMMENT 'kept' CHECK (id > 0),
    owner INT CHECK (owner > 0) CONSTRAINT accepted_owner REFERENCES owners (id),
    other_owner INT CONSTRAINT accepted_other REFERENCES owners (id) ON DELETE CASCADE
);
CREATE TABLE check_then_not_null (x INT CHECK (x > 0) NOT NULL);
CREATE TABLE references_then_check (x INT REFERENCES owners (id) CHECK (x > 0));
CREATE TABLE named_check (x INT CONSTRAINT positive CHECK (x > 0));
CREATE TABLE two_checks (x INT CHECK (x > 0) CHECK (x < 5));
