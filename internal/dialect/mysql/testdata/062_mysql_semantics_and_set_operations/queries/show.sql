-- sgl.query(name: Warnings, command: many)
SHOW WARNINGS;

-- sgl.query(name: WarningCount, command: one)
SHOW COUNT(*) WARNINGS;

-- sgl.query(name: Setting, command: many)
SHOW SESSION VARIABLES LIKE 'sql_mode';

-- sgl.query(name: Tables, command: many)
SHOW FULL TABLES FROM app;

-- sgl.query(name: Definition, command: one)
SHOW CREATE TABLE people;
