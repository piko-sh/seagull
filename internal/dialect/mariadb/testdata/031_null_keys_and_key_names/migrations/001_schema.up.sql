CREATE TABLE gauges (low INT, high INT, CHECK (low < high));
ALTER TABLE gauges RENAME COLUMN low TO minimum;
CREATE TABLE null_key (id INT NULL PRIMARY KEY, name TEXT);
CREATE TABLE modified_key (id INT PRIMARY KEY);
ALTER TABLE modified_key MODIFY id INT NULL;
