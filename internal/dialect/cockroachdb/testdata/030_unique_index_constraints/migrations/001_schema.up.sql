CREATE TABLE boats (id INT PRIMARY KEY, name STRING NOT NULL, berth INT, UNIQUE INDEX (berth));
CREATE UNIQUE INDEX ON boats (name);
CREATE UNIQUE INDEX kept ON boats (name, berth);
ALTER TABLE boats DROP CONSTRAINT boats_name_key;
ALTER TABLE boats DROP CONSTRAINT boats_berth_key;
