CREATE TABLE scores (player String, score UInt64) ENGINE = MergeTree() ORDER BY player;
INSERT INTO scores VALUES ('ann', 30), ('bob', 10), ('cat', 20);
