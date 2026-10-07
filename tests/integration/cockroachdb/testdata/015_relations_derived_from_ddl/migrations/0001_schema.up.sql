CREATE TABLE notes (body STRING NOT NULL, kind INT8);
CREATE TABLE marks (rowid INT8, label STRING);
CREATE TABLE tags (label STRING NOT NULL, colour STRING);
CREATE TABLE labels (name STRING NOT NULL, shade STRING);
CREATE SEQUENCE note_numbers;
