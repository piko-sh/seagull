CREATE TABLE harbours (id INT PRIMARY KEY);
CREATE TABLE inline_without_columns (id INT PRIMARY KEY, harbour INT REFERENCES harbours);
CREATE TABLE table_without_columns (id INT PRIMARY KEY, harbour INT, FOREIGN KEY (harbour) REFERENCES harbours);
CREATE TABLE boats (id INT PRIMARY KEY, harbour INT);
ALTER TABLE boats MODIFY harbour INT REFERENCES harbours (id);
ALTER TABLE boats CHANGE harbour home INT REFERENCES harbours (id);
