CREATE TABLE boats (
    id INT PRIMARY KEY,
    name TEXT NOT NULL,
    harbour TEXT CONSTRAINT harbour_given NOT NULL,
    skipper TEXT,
    berth INT,
    CONSTRAINT boats_crew_not_null CHECK (berth > 0),
    NOT NULL berth
);
ALTER TABLE boats DROP CONSTRAINT boats_name_not_null;
ALTER TABLE boats DROP CONSTRAINT harbour_given;
ALTER TABLE boats ADD CONSTRAINT skipper_given NOT NULL skipper;
ALTER TABLE boats ADD COLUMN crew INT;
ALTER TABLE boats ALTER crew SET NOT NULL;
ALTER TABLE boats DROP CONSTRAINT boats_berth_not_null;
