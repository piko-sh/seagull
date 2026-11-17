CREATE TABLE gauges (
	id INTEGER PRIMARY KEY,
	reading INT CONSTRAINT reading_present NOT NULL,
	level INT NOT NULL,
	depth INT CONSTRAINT depth_positive CHECK (depth > 0),
	scale INT CONSTRAINT scale_default DEFAULT 1 NOT NULL,
	CONSTRAINT level_bound CHECK (level < 100)
);
ALTER TABLE gauges ADD CONSTRAINT level_positive CHECK (level > 0);
ALTER TABLE gauges ADD CHECK (reading > 0);
ALTER TABLE gauges DROP CONSTRAINT reading_present;
ALTER TABLE gauges DROP CONSTRAINT depth_positive;
ALTER TABLE gauges DROP CONSTRAINT level_bound;
ALTER TABLE gauges ALTER COLUMN level DROP NOT NULL;
ALTER TABLE gauges ALTER depth SET NOT NULL;
ALTER TABLE gauges ADD COLUMN offset_value INT CONSTRAINT offset_present NOT NULL DEFAULT 0;
