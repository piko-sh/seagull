CREATE TABLE finds (id UInt64, creature String) ENGINE = MergeTree ORDER BY id;
INSERT INTO finds VALUES (1, 'crab'), (2, 'limpet'), (3, 'whelk'), (4, 'anemone'), (5, 'goby'), (6, 'blenny'), (7, 'prawn'), (8, 'starfish');
