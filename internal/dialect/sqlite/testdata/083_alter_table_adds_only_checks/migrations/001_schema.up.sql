CREATE TABLE gauges (id INTEGER PRIMARY KEY, level INT);
ALTER TABLE gauges ADD CONSTRAINT level_unique UNIQUE (level);
