CREATE TABLE plants (id INTEGER PRIMARY KEY, name TEXT NOT NULL);

CREATE VIEW plant_list AS SELECT * FROM plants;

CREATE VIEW plant_names AS SELECT plants.* FROM plants;

CREATE VIEW plant_stack AS SELECT * FROM plant_list;
