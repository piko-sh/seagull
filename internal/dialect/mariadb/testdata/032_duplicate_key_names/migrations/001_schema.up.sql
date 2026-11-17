CREATE TABLE gauges (id INT PRIMARY KEY, reading INT);
CREATE TABLE twice_indexed (a INT, b INT, INDEX i (a), INDEX i (b));
CREATE INDEX reading_index ON gauges (reading);
CREATE INDEX reading_index ON gauges (id);
