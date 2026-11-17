CREATE TABLE shifts (id INTEGER NOT NULL, current_role INTEGER NOT NULL, localtime INTEGER NOT NULL, "user" INTEGER NOT NULL, current_date INTEGER NOT NULL);
CREATE TABLE crew (id INTEGER NOT NULL, name VARCHAR);
CREATE VIEW shift_days AS SELECT current_date, current_role AS role_number FROM shifts;
CREATE VIEW crew_days AS SELECT current_date AS today FROM crew;
