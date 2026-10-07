CREATE TABLE catches (
    id INTEGER PRIMARY KEY,
    weight INTEGER NOT NULL,
    species VARCHAR
);

ALTER TABLE catches ALTER COLUMN weight TYPE DOUBLE;

ALTER TABLE catches ALTER species SET DATA TYPE VARCHAR[];

CREATE TABLE heavy_catches AS SELECT id, weight, weight * 2 AS doubled FROM catches WHERE weight > 10;
