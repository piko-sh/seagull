CREATE TABLE harbours (id INT PRIMARY KEY);
CREATE TABLE boats (
    id INT PRIMARY KEY,
    harbour INT REFERENCES harbours (id),
    home INT,
    winter INT,
    FOREIGN KEY (home) REFERENCES harbours (id),
    CONSTRAINT `7` FOREIGN KEY (winter) REFERENCES harbours (id)
);
ALTER TABLE boats DROP FOREIGN KEY `1`;
ALTER TABLE boats ADD COLUMN spring INT REFERENCES harbours (id), ADD FOREIGN KEY (harbour) REFERENCES harbours (id);
RENAME TABLE boats TO vessels;
