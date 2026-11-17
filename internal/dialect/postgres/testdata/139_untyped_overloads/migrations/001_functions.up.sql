CREATE FUNCTION scale(integer) RETURNS integer LANGUAGE sql AS 'SELECT 1';
CREATE FUNCTION pick(double precision) RETURNS double precision LANGUAGE sql AS 'SELECT $1';
CREATE FUNCTION pick(text) RETURNS text LANGUAGE sql AS 'SELECT $1';
CREATE FUNCTION weigh(integer) RETURNS integer LANGUAGE sql AS 'SELECT $1';
CREATE FUNCTION weigh(numeric) RETURNS numeric LANGUAGE sql AS 'SELECT $1';
CREATE FUNCTION weigh(double precision) RETURNS double precision LANGUAGE sql AS 'SELECT $1';
