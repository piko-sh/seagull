-- sgl.query(name: DivideByZero, command: one)
SELECT 1 / 0 AS nothing;

-- sgl.query(name: Warnings, command: many)
SHOW WARNINGS;

-- sgl.query(name: WarningCount, command: one)
SHOW COUNT(*) WARNINGS;

-- sgl.query(name: Setting, command: many)
SHOW SESSION VARIABLES LIKE 'sql_mode';

-- sgl.query(name: Tables, command: many)
SHOW FULL TABLES;

-- sgl.query(name: Definition, command: one)
SHOW CREATE TABLE archived_people;
