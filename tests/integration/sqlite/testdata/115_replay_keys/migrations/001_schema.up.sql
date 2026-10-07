CREATE TABLE plain (code TEXT PRIMARY KEY, note TEXT);
CREATE TABLE IF NOT EXISTS plain (id INTEGER PRIMARY KEY);
CREATE TABLE pairs (left_id INTEGER, right_id INTEGER, note TEXT, PRIMARY KEY (left_id, right_id));
CREATE TABLE strict_keys (code TEXT PRIMARY KEY, note TEXT) STRICT;
CREATE TABLE numbered (id INTEGER PRIMARY KEY, note TEXT);
CREATE VIEW IF NOT EXISTS plain_view (plain_code, plain_note) AS SELECT * FROM plain;
CREATE VIEW IF NOT EXISTS plain_view AS SELECT code FROM plain;
