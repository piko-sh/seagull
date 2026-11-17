CREATE TABLE boats (
    id INTEGER CONSTRAINT boat_key PRIMARY KEY,
    name TEXT CONSTRAINT boat_name UNIQUE,
    berth INT,
    crew INT CONSTRAINT crew_present NOT NULL DEFAULT 0,
    CONSTRAINT boat_berth UNIQUE (name, berth)
);
CREATE TABLE crews (id INT, CONSTRAINT crew_key PRIMARY KEY (id));
ALTER TABLE boats DROP CONSTRAINT boat_key;
ALTER TABLE boats DROP CONSTRAINT boat_name;
ALTER TABLE boats DROP CONSTRAINT boat_berth;
ALTER TABLE crews DROP CONSTRAINT crew_key;
ALTER TABLE boats DROP CONSTRAINT no_such_name;
ALTER TABLE boats DROP CONSTRAINT crew_present;
ALTER TABLE boats DROP CONSTRAINT crew_present;
