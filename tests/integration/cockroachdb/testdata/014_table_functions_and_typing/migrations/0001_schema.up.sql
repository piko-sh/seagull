CREATE TYPE pair AS (a INT, b TEXT);
CREATE TABLE users (id INT PRIMARY KEY, name TEXT NOT NULL, bar pair);
CREATE TABLE results (id INT8 PRIMARY KEY, label TEXT NOT NULL);
CREATE FUNCTION users_named(p TEXT) RETURNS SETOF users LANGUAGE sql STABLE AS $$ SELECT * FROM users WHERE name = p $$;
CREATE FUNCTION two_outs(IN x INT, OUT doubled INT, OUT label TEXT) LANGUAGE sql IMMUTABLE AS 'SELECT x * 2, ''n''';
CREATE FUNCTION one_out(IN x INT, OUT total INT) LANGUAGE sql IMMUTABLE AS 'SELECT x * 2';
CREATE PROCEDURE create_user(IN p_name TEXT, OUT p_id INT) LANGUAGE plpgsql AS $$ BEGIN INSERT INTO users (id, name) VALUES (1, p_name); p_id := 1; END $$;
