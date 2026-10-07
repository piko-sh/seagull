CREATE TABLE gulls (id UInt64, name String, beach String) ENGINE = MergeTree ORDER BY id;
INSERT INTO gulls VALUES (1, 'Gerald', 'Porthcurno'), (2, 'Hilda', 'Porthcurno'), (3, 'Ivor', 'Sennen'), (4, 'Jean', 'Sennen');
