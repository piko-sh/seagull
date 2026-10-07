CREATE TABLE gulls (id UInt64, name String) ENGINE = MergeTree ORDER BY id;
CREATE TABLE sightings (gull_id UInt64, beach String, count UInt32, note Nullable(String)) ENGINE = MergeTree ORDER BY gull_id;
INSERT INTO gulls VALUES (1, 'Gerald'), (2, 'Hilda');
INSERT INTO sightings VALUES (1, 'Porthcurno', 3, 'windy');
