CREATE TABLE plants (id INTEGER PRIMARY KEY, name VARCHAR NOT NULL, stock INTEGER NOT NULL);

CREATE VIEW plant_list AS SELECT * FROM plants;

CREATE VIEW plant_names (plant_id, plant_name) AS SELECT * FROM plants;
