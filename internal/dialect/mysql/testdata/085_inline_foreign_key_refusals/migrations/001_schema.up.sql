CREATE TABLE harbours (id INT PRIMARY KEY);
CREATE TABLE trailing_not_null (id INT PRIMARY KEY, harbour INT REFERENCES harbours (id) NOT NULL);
CREATE TABLE trailing_check (id INT PRIMARY KEY, harbour INT REFERENCES harbours (id) CHECK (harbour > 0));
CREATE TABLE named_reference (id INT PRIMARY KEY, harbour INT CONSTRAINT boat_harbour REFERENCES harbours (id));
CREATE TABLE boats (id INT PRIMARY KEY, harbour INT);
ALTER TABLE boats MODIFY harbour INT REFERENCES harbours (id);
ALTER TABLE boats CHANGE harbour home INT REFERENCES harbours (id);
