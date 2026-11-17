CREATE TABLE a (k Int32, v Int32) ENGINE = MergeTree ORDER BY k;
CREATE VIEW full_list (x, y) AS SELECT k, v FROM a;
