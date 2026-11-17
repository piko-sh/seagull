CREATE TABLE harbours (id INTEGER PRIMARY KEY);
CREATE TABLE boats (
    id INTEGER PRIMARY KEY,
    harbour INT CONSTRAINT boat_harbour REFERENCES harbours (id),
    berth INT,
    crew INT CONSTRAINT crew_default DEFAULT 1,
    name TEXT CONSTRAINT name_collation COLLATE NOCASE,
    CONSTRAINT boat_berth FOREIGN KEY (berth) REFERENCES harbours (id)
);
ALTER TABLE boats DROP CONSTRAINT boat_harbour;
ALTER TABLE boats DROP CONSTRAINT boat_berth;
ALTER TABLE boats DROP CONSTRAINT crew_default;
ALTER TABLE boats DROP CONSTRAINT name_collation;
