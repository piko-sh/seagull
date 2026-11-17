CREATE TABLE harbours (id INT PRIMARY KEY);
CREATE TABLE boats (
    id INT PRIMARY KEY,
    harbour INT REFERENCES harbours (id) ON DELETE CASCADE,
    home INT CHECK (home > 0) REFERENCES harbours,
    berth INT UNIQUE REFERENCES harbours (id)
);
ALTER TABLE boats ADD COLUMN winter INT REFERENCES harbours (id) AFTER home;
ALTER TABLE boats ADD COLUMN (spring INT REFERENCES harbours (id), autumn INT);
RENAME TABLE boats TO vessels;
